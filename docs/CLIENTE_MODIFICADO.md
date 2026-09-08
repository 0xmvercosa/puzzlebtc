# O cliente modificado

Um participante que apaga o trecho de resgate do cliente fica com o prêmio
inteiro se a chave cair no lote dele. Este documento registra o que foi
investigado contra isso, o que funciona, e o que foi descartado com o motivo.

## Por que não existe detecção técnica barata

**Hash do binário reportado pelo cliente.** O cliente modificado devolve o hash do
original. Uma linha. Toda atestação sem raiz de confiança em hardware pede que a
parte não confiável faça um relatório sobre si mesma.

**Anti-cheat no estilo de jogo.** Funciona lá porque o servidor observa o
resultado: quem mira bem demais gera estatística impossível. Aqui o cliente
modificado produz saída idêntica à do honesto — mesmas testemunhas, mesmas provas
válidas — até o único instante em que importa. Não há sinal comportamental. E
todo DRM e anti-cheat existente já foi quebrado por prêmios muito menores que
este.

**Testar montagem e assinatura sem transmitir.** O modificado assina o teste
honestamente e pula o broadcast só no prêmio. Testa a etapa errada.

**Atestação por TPM.** Funciona de verdade, e exclui Mac, boa parte do Linux,
toda máquina virtual e hardware antigo. Trocaria o risco de roubo pelo pool não
existir.

**Canário de resgate.** Plantar uma chave de um endereço financiado e conferir na
cadeia se as moedas se moveram. É o único mecanismo que funciona, porque a
transmissão é a única etapa que não dá para falsificar. Está implementado em
`internal/coordinator/canary.go` e **desligado por padrão**: exige capital de giro
parado e taxa de transação contínua, e um atacante que consulte o saldo antes de
decidir resgata os canários pequenos e guarda a chave só no prêmio grande.

**Teto de fatia por identidade.** Derrubado por Sybil: a fazenda cria duzentas
identidades de uma placa cada e a fatia total dela não muda. Identidade é grátis.

## Onde o risco realmente está

O participante decide modificar **antes** de achar qualquer coisa. A chance de ele
ser quem encontra é a fatia dele do trabalho do pool, então o ganho esperado do
ataque é fatia × prêmio:

| perfil | fatia do pool | ganho esperado de atacar |
|---|---|---|
| 1 notebook | 0,002% | US$ 11 |
| 1 placa média | 0,02% | US$ 114 |
| 1 RTX 4090 | 0,1% | US$ 568 |
| rig de 6 placas | 0,6% | US$ 3.408 |
| fazenda de 50 placas | 5% | US$ 28.400 |
| fazenda de 200 placas | 20% | US$ 113.600 |

Com prêmio de US$ 568.000 e um pool de mil placas equivalentes. E este é o ganho
**bruto**: o esperado de verdade é esse número vezes a chance de o roubo se
converter em dinheiro mantido, que para quem não é minerador fica bem abaixo de
metade — ver [`DISSUASAO.md`](DISSUASAO.md).

**A maioria absoluta dos participantes não tem incentivo.** Ninguém constrói um
bypass por onze dólares de expectativa. O risco vive quase inteiramente nas poucas
fazendas grandes.

## O que de fato se faz

**Nada, para a cauda longa.** A aritmética já defende. Gastar dinheiro testando
dez mil participantes cujo incentivo é de dois dígitos seria pagar caro pelo lugar
errado.

**Conhecer os contribuidores grandes.** Uma fazenda de duzentas placas é uma
conversa, não um download anônimo. Saber quem são os poucos que representam fatia
significativa é a defesa que funciona nessa faixa, e não custa nada. É controle
operacional, não técnico, e é assim que isso é tratado na prática.

**Distribuir binário assinado com build reproduzível.** Não impede modificação;
torna rodar o oficial o caminho fácil, que é o que a maioria absoluta faz.

**Registro público de quem tinha cada lote.** Os endereços do puzzle estão entre
os mais observados do Bitcoin. Moeda que se mova sem ninguém reportar identifica
na hora quem estava com aquele terreno, e gastar moeda marcada de um endereço
famoso é um problema real e permanente.

**A fatia de quem encontra.** É a alavanca mais direta que existe, e é uma escolha
aberta: com fatia `f`, desertar só compensa se a chance de converter o roubo
passar de `f`. Aos 50% de hoje a barra está em 50%; a 70% ela estaria em 70%, ao
custo de 20 pontos tirados da plataforma ou dos ajudantes.

## O que sobrou depois de descartar tudo isso

Todas as opções acima tentam **impedir** ou **detectar** o roubo, e todas falham.
O que restou faz outras três perguntas, e nenhuma delas depende de confiar no
participante.

**Não entregar a chave.** O lote é entregue como ponto da curva, não como faixa
de chaves. O cliente anda `A, A+G, A+2G, …`, hasheia pontos e reporta o offset;
quem calcula `a + offset` é o coordenador. Não há chave na máquina do participante
para ser guardada, nem lendo o código, nem despejando a memória. Para virar chave
é preciso um logaritmo discreto sobre a faixa da campanha, que é um segundo
ataque, deliberado, que não vem no cliente. Está em `internal/blind`.

**A conta, que não precisa de ameaça.** Com 50% para quem encontra, desertar só
compensa se o desertor tiver mais de 50% de chance de converter o roubo em
dinheiro que ele mantém. Isso sai da álgebra, não de um compromisso do pool: a
fatia de ajudante que ele continua recebendo é a mesma nos dois casos e se
cancela.

**Por que essa chance é baixa.** Transmitir publica a chave pública na assinatura,
e dali a privada sai em onze segundos num rig contra dez minutos de bloco. Pela
mempool pública ele perde para o ecossistema de front-running que já existe. Por
relay privado ele entrega meio milhão de dólares a uma empresa que pode
simplesmente levar. Minerando o próprio bloco funciona — e exige ser minerador.

**A atribuição.** O lease é assinado antes de o lote ser varrido. Quando a chave
aparece na cadeia, `BlockIndexOf` diz em qual lote ela estava e o lease diz quem
estava com ele: correspondência aritmética verificável, publicada antes do crime.

Nenhuma delas exige capital de giro, nenhuma exige leilão de taxa, e nenhuma
custa nada a quem é honesto.

Detalhes, números e limites: [`DISSUASAO.md`](DISSUASAO.md).

## O risco residual, dito com todas as letras

Um desertor que **seja minerador** transmite pelo próprio bloco e fica com a
chave. Não há mecanismo neste repositório que impeça isso, e o README diz isso ao
participante em vez de fingir garantia.

O que existe é: a maioria não tem motivo, os poucos que têm são conhecíveis, o
caminho honesto é o padrão, o desvio exige um ataque deliberado a mais, a traição
é atribuível — e, para todo desertor que não seja minerador, ela rende menos que
colaborar.

E o dano de quem desertar mesmo assim é limitado pelo tamanho dele: um desertor só
leva a chave se ela estiver no terreno que ele mesmo varreu, então em valor
esperado ele tira do pool exatamente a fração de trabalho que fez. Um pool que
fosse metade desertores ainda pagaria 86% ao participante honesto.
