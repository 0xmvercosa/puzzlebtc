# Otimizações mapeadas

Levantamento com esforço e ganho de cada mudança candidata, para decidir o que
vale fazer. **Todos os números marcados como medidos foram medidos neste
repositório**, com benchmark contra o código real, não estimados.

Ordenado por ganho pelo esforço.

## Fazer

### 1. Contadores no lugar de `COUNT(*)` no `Stats()`

**Este é o único muro real do projeto.** Com 10.000 participantes consultando
progresso uma vez por minuto contra uma tabela de 1 milhão de linhas, o banco
precisa de **49 segundos de trabalho por segundo de relógio**. O sistema para no
segundo dia.

Medido: 3,3 ms com 10 mil blocos, 47,6 ms com 100 mil, 373 ms com 1 milhão,
1.474 ms com 4 milhões. Vira leitura constante abaixo de 100 µs.

Piora porque `MaxOpenConns(1)` serializa tudo: cada consulta lenta de progresso
trava todo aluguel e toda submissão atrás dela.

**Esforço:** meio dia. O difícil não são os contadores, é garantir que toda via
de mutação os atualize dentro da transação existente, com teste que recalcula e
compara. **Risco:** contador dessincronizado reporta progresso errado em
silêncio; mitiga com o teste de recálculo e um comando de reconstrução.

### 2. Corrida de dados no sorteio de lotes

Não é ganho de velocidade, é correção. O `go test -race` acusava assim que o
segundo participante conectava. Fonte de aleatoriedade corrompida pode repetir
índice, e repetir índice quebra exatamente a garantia que o projeto vende.

**Já corrigido**, com teste de regressão que roda oito workers em paralelo.

### 3. Caminhos rápidos de `uint64` na verificação

Medido: laço de estrutura 347 µs → 15 µs (23 vezes); contagem de buckets
1.116 µs → 52 µs (21,5 vezes). Juntos, 1,46 ms → 0,07 ms por submissão, com as
alocações caindo de 65.543 para um punhado. É mais do que custa a matemática de
curva que eles protegem.

**Esforço:** uma ou duas horas, cerca de 20 linhas, protegidas por `IsUint64()`.

### 4. Trocar `TargetWitnesses` de 16384 para 4096

Melhor ganho por caractere do repositório: **4 vezes em quatro custos ao mesmo
tempo.** Wire 212 KB → 53 KB, parse de JSON 3,37 ms → 0,84 ms, contagem de
buckets e laço de estrutura 4 vezes mais baratos, auditoria profunda 311 ms →
78 ms.

**Custo:** o menor buraco detectável de cobertura sobe de 0,2% para 0,8% do lote.
Quem cortar 0,8% da varredura economiza 0,8% de energia, então a perda econômica
é desprezível.

**A armadilha:** os buckets precisam cair junto. Mantendo 512 buckets com 4096
testemunhas, a média por bucket vai para 8, o piso de Poisson colapsa para 1, e
**cerca de 17% das submissões honestas passam a ser recusadas por engano.**

### 5. Descartar `rand.Perm` na amostragem

Medido: 176 µs → 2,7 µs (66 vezes) e 137 KB → 2,8 KB alocados por submissão.

**Cuidado:** a amostra precisa continuar reproduzível a partir de (segredo,
campanha, lote) para resolver disputa. Mantém o mesmo HMAC e fixa a nova ordem
com teste de referência.

### 6. Verificação de testemunhas com inversão em lote

Medido: 19.041 ns → 3.925 ns por chave, 4,85 vezes. Auditoria profunda de um
lote inteiro cai de 311 ms para 64 ms de CPU travada dentro de um handler HTTP.

**Esforço:** um dia. **Risco moderado:** aritmética de campo escrita à mão é onde
moram bugs sutis, e um verificador errado ou recusa trabalho honesto ou aceita
testemunha forjada. Mitigação: teste diferencial contra a implementação canônica,
rodando em CI.

### 7. Varredura incremental em lote no worker de referência

Medido: **13,9 vezes.** 18.720 ns → 1.349 ns por chave, ou seja 53.419 → 741.300
chaves/s por núcleo.

Honestidade sobre o que isso vale: contra uma RTX 4090 continua sendo 0,01% de
uma GPU, e **não** faz o worker de CPU conseguir fechar um lote grande. O valor é
tornar a CPU um alternativa crível, servir de alvo diferencial rápido em CI, e
fechar uma diferença de 20 vezes contra o `btcgoai` que a nossa própria pesquisa
já apontava.

**Risco moderado:** varredura errada perde o prêmio em silêncio, que é a pior
falha possível aqui. Mantém a implementação simples como referência canônica.

### 8. Paralelizar a varredura entre núcleos

Quase linear. Somado ao item anterior: 53.419 → 2,96 milhões de chaves/s, cerca
de **55 vezes**. De quebra, torna a barra de progresso do cliente trivial de
derivar.

**Esforço:** meio dia, cerca de 40 linhas.

### 9. Separar pools de leitura e escrita no banco

Elimina o bloqueio de fila: nenhuma leitura, por mais lenta, trava aluguel ou
submissão. **Esforço:** duas horas.

### 10. Tirar `ReclaimExpired` do caminho de aluguel

Seguro contra uma regressão medida de **13.800 vezes**: o mesmo `DELETE` sem
efeito vai de 0,01 ms para 138 ms com 1 milhão de linhas, se o `ANALYZE` rodar
enquanto todas as linhas compartilham o mesmo estado, que é o regime normal.

## Talvez

**Formato binário para as testemunhas.** Medido: 212.603 → 65.283 bytes (3,26
vezes) e parse 3.371 µs → 430 µs (7,8 vezes). O ganho de parse é real; o de banda
é quase inútil, porque 3 TB/mês de entrada é grátis em qualquer nuvem. O
problema: são dois dias de trabalho para atacar o terceiro maior custo, enquanto
o item 4 entrega 4 vezes do mesmo com uma linha.

**Arquivar lotes concluídos.** Medido 184–191 bytes por lote. Com 10.000
participantes são ~33 GB e 175 milhões de linhas por ano. Ganho de disco, não de
latência. **Risco:** apagar o rastro de quem varreu o quê, num projeto cujo
argumento é transparência. Se for fazer, exporta antes de compactar.

## Não fazer

**Comprimir o corpo da submissão com gzip.** Medido 2,08 vezes de compressão, mas
a descompressão custa 3.098 µs em cima dos 3.371 µs de parse: **dobra a CPU do
coordenador**. Gasta o recurso escasso para economizar o abundante, e abre
superfície de zip bomb que o limite de 8 MB não cobre.

**Protocolo de compromisso Merkle com desafio.** Não fica menor que a codificação
simples, custa uma semana, e destrói a propriedade que faz o desenho atual bom:
verificação em uma rodada, com o coordenador sem guardar nada por submissão
pendente.

**RIPEMD-160 em SIMD.** Teto de 1,4 vez no total, ao custo de semanas de assembly
AVX2 mais fallback para arm64. Seria o único código não portável num repositório
que hoje é Go puro sem cgo, que é o que torna trivial compilar para os três
sistemas.

**Migrar para Postgres.** Sem ganho em escala plausível: a capacidade de escrita
medida do SQLite é de ~1.745 pares aluguel+submissão por segundo, o que dá mais
de 3 milhões de participantes. Os dois problemas que se costuma culpar no SQLite
aqui são resolvidos pelos itens 1 e 9, por uma fração do custo. Migrar troca um
binário autocontido por um serviço com banco para operar, e move o livro de
pagamento, que é a coisa que nunca pode corromper.

## Resumo para decidir

| # | mudança | ganho | esforço |
|---|---|---|---|
| 1 | contadores no `Stats()` | remove o único muro do projeto | meio dia |
| 4 | `TargetWitnesses` 16384 → 4096 | 4× em quatro custos | 1 hora |
| 3 | caminhos `uint64` na verificação | 21× | 2 horas |
| 5 | sem `rand.Perm` | 66× | 1 hora |
| 8 | varredura paralela | 4× por núcleo | meio dia |
| 10 | `ReclaimExpired` fora do aluguel | seguro contra 13.800× | 1 hora |
| 9 | pools de leitura e escrita | remove bloqueio de fila | 2 horas |
| 6 | inversão em lote na verificação | 4,85× | 1 dia |
| 7 | varredura incremental | 13,9× | 2 a 3 dias |

Os cinco primeiros somam menos de dois dias e resolvem o muro de escala mais os
maiores custos por submissão.
