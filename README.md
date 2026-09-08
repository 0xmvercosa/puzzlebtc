# puzzlebtc

Coordenador de busca distribuída para o [Bitcoin Puzzle](https://privatekeys.pw/puzzles/bitcoin-puzzle-tx).
Divide o keyspace em blocos, entrega um bloco aleatório a cada participante, e
**verifica que o bloco foi de fato varrido por inteiro** antes de creditar o
ticket que dá direito a uma fatia do prêmio.

A parte difícil não é dividir o trabalho — é provar que ele foi feito. Um pool
que aceita "varri, não achei nada" na palavra do participante paga tickets para
quem não gastou um watt. Este repositório resolve isso primeiro; o resto é
encanamento.

## Por que um pool

Os números, medidos e não estimados:

| | chaves/s | puzzle #71 sozinho |
|---|---|---|
| CPU (1 core, Go) | 55 mil | 680 milhões de anos |
| GPU (RTX 3050, `cacagpu`) | 650 milhões | 57 mil anos |
| GPU (RTX 5090, `CUDACyclone`) | 8,4 bilhões | **4.460 anos** |

Uma GPU sozinha não resolve #71 — nem em mil vidas. Mil delas resolvem em ~4,5
anos; dez mil, em ~163 dias. **A agregação é a única coisa que torna o problema
tratável**, e é exatamente por isso que a contabilidade de quem varreu o quê
precisa ser à prova de fraude.

Os números acima foram medidos, não estimados — a metodologia e a aritmética
completa estão em [`docs/research/benchmarks.md`](docs/research/benchmarks.md).

## Como a prova de varredura funciona

Duas metades independentes, ambas O(1) para o coordenador verificar.

### Testemunhas — provam volume e cobertura

Uma chave é **testemunha** quando seu HASH160 tem pelo menos `witness_bits` bits
zero à esquerda. Testemunhas são raras (1 em 2^`witness_bits`) e **não existe
atalho para produzir uma que não seja hashear chaves até achar**.

Isso é o pulo do gato: quem varre o bloco de verdade as encontra de graça — é a
mesmíssima comparação que o kernel já faz contra o alvo — enquanto quem forja
precisa gastar, em média, **exatamente o custo honesto por testemunha
inventada**. Forjar a prova nunca é mais barato que fazer o trabalho.

- **Contar** testemunhas limita *quanto* do bloco foi varrido.
- **Exigir testemunhas em cada bucket** limita *onde*. Com os defaults, um buraco
  de um bucket (0,2% do bloco) deixa aquele bucket vazio e é rejeitado na hora,
  enquanto uma submissão honesta é rejeitada com probabilidade abaixo de 1e-8.
- O coordenador ainda verifica **uma amostra aleatória** com matemática de curva
  de verdade, então mandar offsets fabricados que apenas *parecem* bem
  distribuídos também falha.

Quais offsets entram na amostra deriva de um segredo do servidor — o participante
não sabe quais pode se dar ao luxo de falsificar.

### Canários — provam que o caminho de reporte funciona

Testemunhas provam que chaves foram hasheadas. Não provam que o participante
*avisaria* alguém ao achar algo. Então o coordenador deriva alguns offsets
"canário" de um segredo (sem armazenar nada — são recalculados na verificação) e
inclui o HASH160 deles na watchlist entregue junto com o alvo real. Um worker com
o caminho de reporte quebrado, desligado ou stubado coleta testemunhas
normalmente e **ainda assim falha em devolver os canários**.

### O que isso não faz

Nada aqui obriga quem encontra a chave a reportá-la. O endereço do puzzle é
público, então o participante sempre consegue distinguir o acerto real de um
canário. Ver [Modelo de confiança](#modelo-de-confiança).

## Rateio

Padrão: **50% quem encontra / 30% plataforma / 20% dividido entre quem ajudou**,
pro rata por tickets. Um ticket por bloco verificado, um bloco nunca gera dois.

Toda a aritmética é em satoshis inteiros — `math/big`, nunca float. Um float64
não representa todos os valores de satoshi acima de 2^53, e um erro de
arredondamento de 1 satoshi por participante é o tipo de bug que destrói a
confiança num pool para sempre. A divisão é exaustiva por construção: o resto da
divisão inteira é distribuído por maior resto, e
`Finder + Platform + Σ Helpers == prêmio` exatamente. Há um teste que varre
prêmios e distribuições de ticket confirmando isso.

Se ninguém além de quem encontrou tiver tickets, os 20% vão para ele em vez de
ficarem órfãos.

## Rodando

```bash
export PUZZLEPOOL_SECRET="pelo menos 32 bytes, estável entre restarts"

go run ./cmd/coordinator \
  -puzzle 71 \
  -target-hash160 <40 hex do endereço alvo> \
  -block-bits 40 \
  -db pool.db -addr :8080
```

Em outro terminal:

```bash
go run ./cmd/worker \
  -server http://localhost:8080 -id alice \
  -target-hash160 <mesmo alvo> -blocks 1
```

O worker de referência é CPU pura e lento de propósito (~55 mil chaves/s por
core). Ele existe para **definir o protocolo sem ambiguidade** e para dar a um
worker de GPU algo contra o que fazer diff: aponte os dois para o mesmo bloco
pequeno e as duas submissões têm que sair idênticas.

Um worker de GPU só precisa reproduzir três comportamentos — ver
[`docs/PROTOCOL.md`](docs/PROTOCOL.md).

## Pesquisa anterior

Antes de escrever o coordenador, analisei as quatro ferramentas existentes de
busca no puzzle — `btcgo`, `btcgoai`, `cacagpu` e `CUDACyclone`. Duas conclusões
viraram decisão de projeto: **nenhuma sabe onde parou** (nenhuma tem checkpoint) e
**nenhuma tem como provar que varreu** — todas confiam na própria execução, porque
são ferramentas de um usuário só.

Ver [`docs/research/`](docs/research/) para a análise completa, os bugs
encontrados em cada uma, e o que este projeto reaproveita delas.

## Arquitetura

```
internal/btc/          derivação HASH160 de referência (CPU)
internal/keyspace/     range → blocos; tabela de blocos é esparsa, nunca materializada
internal/proof/        testemunhas, canários, verificação, sweeper de referência
internal/payout/       rateio exato em satoshis inteiros
internal/store/        SQLite: campanhas, leases, tickets, soluções
internal/coordinator/  alocação aleatória, verificação, API HTTP
cmd/coordinator/       o servidor
cmd/worker/            o participante de referência
docs/PROTOCOL.md       o contrato worker <-> coordenador
docs/research/         análise das ferramentas existentes e benchmarks medidos
```

**A tabela de blocos é esparsa de propósito.** Puzzle #71 com blocos de 2^40
chaves tem 2^30 blocos; puzzles maiores são muito piores. Materializar uma linha
por bloco é impossível. Então só existe linha depois que o bloco foi alugado, e
"disponível" significa "não tem linha". A alocação sorteia um índice uniforme e
deixa a primary key rejeitar a colisão — que para qualquer campanha real é
astronomicamente rara. O custo de entregar um bloco não cresce com o tamanho da
campanha.

**A alocação é aleatória, não sequencial.** Isso não é detalhe de implementação:
entrega sequencial deixaria o participante prever o próximo bloco e pré-computá-lo,
e tornaria o progresso do pool trivialmente observável por um concorrente.

## Modelo de confiança

Escrito aqui porque quem entra num pool merece saber no que está entrando.

**1. Quem encontra pode simplesmente não avisar.** É impossível impedir
criptograficamente. O worker calcula a chave na própria máquina; nenhum protocolo
tira isso dele. As defesas são econômicas, não matemáticas: os 50% para quem
encontra existem justamente para que reportar seja a jogada racional, e o
movimento das moedas é público — se o endereço do puzzle for gasto sem que
ninguém reporte, o participante que tinha aquele bloco alugado é identificável.
Isso é detecção posterior, não prevenção.

**2. A plataforma é confiável, e isso é uma escolha.** Ela guarda o segredo dos
canários, aloca os blocos e mantém o livro de tickets. Um participante precisa
confiar que vai ser pago. Uma versão futura pode publicar o ledger de tickets ou
mover o custódia para multisig; hoje, não.

**3. O segredo do canário é crítico.** Quem o tiver prevê os canários de qualquer
bloco e passa nesse teste sem varrer nada. Ele nunca pode chegar a um worker.
Trocá-lo invalida todo lease em aberto.

**4. Antes de aceitar dinheiro de terceiros, consulte um advogado.** Distribuir
prêmio entre participantes pode ser enquadrado como loteria, jogo ou oferta de
valores mobiliários dependendo da jurisdição. Isto não é aconselhamento jurídico
— é um aviso de que a questão existe e é anterior ao código.

**5. A aritmética continua brutal.** Mesmo com 10 mil GPUs de topo, #71 leva
~163 dias e a chance de sucesso em qualquer janela é proporcional à fração do
keyspace varrida. Nenhum participante deve entrar esperando retorno.

## Estado

Funciona ponta a ponta hoje: alugar bloco → varrer → provar → ticket → rateio,
com verificação real e um teste que confirma que submissão forjada é rejeitada.

| Verificado | |
|---|---|
| Varredura honesta aceita | ✅ |
| Varredura de 90% rejeitada (buraco de cobertura) | ✅ |
| Testemunhas fabricadas rejeitadas | ✅ |
| Testemunhas duplicadas rejeitadas | ✅ |
| Caminho de reporte quebrado rejeitado | ✅ |
| Prêmio falso rejeitado | ✅ |
| Lease expirado não pode ser resgatado | ✅ |
| Bloco alheio não pode ser resgatado | ✅ |
| Submissão dupla não gera dois tickets | ✅ |
| Bloco varrido nunca é reentregue | ✅ |
| Rateio fecha exatamente no prêmio | ✅ |

Falta, em ordem de importância:

1. **Autenticação de worker.** Hoje `worker_id` é auto-declarado — qualquer um
   credita tickets em qualquer nome. Precisa de chave por participante e
   assinatura na submissão. **Isto é bloqueante para qualquer campanha real.**
2. **Stake e reputação.** Prova rejeitada hoje não custa nada ao participante;
   dá para tentar até acertar. Falha em auditoria profunda deveria queimar stake.
3. **Worker de GPU.** O de referência é 10.000× lento demais para uso sério. O
   protocolo já está definido para portar `cacagpu` ou `CUDACyclone`.
4. **Custódia e pagamento.** Não há nada aqui que mova satoshi nenhum.
5. **Postgres.** SQLite serve um coordenador; não serve vários.
