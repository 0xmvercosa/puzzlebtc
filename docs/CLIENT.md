# O cliente

Desenho do programa que o participante roda. Ainda não construído; este documento
é a especificação.

## Medindo a máquina antes de começar

Na primeira execução o cliente roda um benchmark curto, de poucos segundos, que
varre um intervalo conhecido e mede chaves por segundo reais naquela máquina.
Estimar por modelo de GPU não funciona: clock, temperatura, driver e o que mais
estiver rodando mudam o resultado com facilidade.

O resultado fica em cache e é refeito quando o hardware muda ou a pedido.

Com a medição em mãos, o cliente responde a pergunta que interessa: **em quanto
tempo esta máquina fecha um lote, e quantos lotes ela fecha no tempo que você
escolher.**

```
$ puzzlebtc bench

  Medindo...

  GPU        NVIDIA GeForce RTX 4060
  Velocidade 1,24 Gchaves/s
  Lote       6,9 s

  Em 1h  voce fecha      519 lotes    US$ 0,0015
  Em 2h  voce fecha    1.039 lotes    US$ 0,0030
  Em 6h  voce fecha    3.116 lotes    US$ 0,0090
  Em 12h voce fecha    6.233 lotes    US$ 0,0180

  Campanha ativa: puzzle #71 — premio 7,1 BTC (US$ 568.000)
  Valor esperado por lote: US$ 0,0000029

  Isso e valor esperado de loteria, nao rendimento: voce so recebe se
  o pool encontrar a chave. Nao ha pagamento por tempo rodado.
```

## Escolhendo quanto rodar

```bash
puzzlebtc run --for 1h      # roda por uma hora e para
puzzlebtc run --for 2h
puzzlebtc run --for 6h
puzzlebtc run --for 12h
puzzlebtc run --blocks 20   # roda vinte lotes e para
puzzlebtc run               # roda ate voce parar
```

Ao terminar o tempo escolhido o cliente finaliza o lote em andamento antes de
sair, se faltar pouco. Se faltar muito, ele abandona o lote e sai limpo, avisando
o que foi descartado.

## Tamanho do lote

O lote é o mesmo para todo mundo, dimensionado para o que uma **CPU comum** fecha
em cerca de uma hora. Quem tem mais capacidade não recebe lote maior: recebe mais
lotes, e trabalha vários ao mesmo tempo se tiver mais de uma GPU.

Isso é o que mantém o ticket com o mesmo significado para todo mundo. Lote maior
para quem tem máquina melhor quebraria a proporcionalidade do rateio, e a
contabilidade perderia o sentido.

Na campanha padrão o lote tem 2³³ chaves, cerca de 8,6 bilhões:

| hardware | tempo por lote | lotes por dia |
|---|---|---|
| CPU 4 núcleos | ~68 min | ~21 |
| GTX 1660 / RTX 3050 | ~13 s | ~6.540 |
| RTX 4060 | ~7 s | ~12.500 |
| RTX 4090 | ~1,4 s | ~62.500 |
| Rig com 6 placas | ~0,2 s | ~375.000 |

O tamanho é escolhido pela CPU: uma máquina comum fecha um lote em cerca de uma
hora, sem precisar deixar ligado a noite toda para ver o primeiro ticket.

Quem tem placa fecha lote em segundos, e por isso o cliente pede lotes **em
bloco**, cinquenta ou cem por requisição. Sem isso uma placa topo de linha abriria
62 mil conexões por dia. O lote continua do mesmo tamanho para todo mundo; o que
muda é quantos você leva de cada vez.

## Pausar no meio de um lote

Um lote pela metade não gera ticket, e não dá para consertar isso. A prova de
varredura exige testemunhas espalhadas por todo o lote; meio lote não passa na
verificação, por construção. Aceitar meia prova seria abrir exatamente o buraco
que o projeto existe para fechar.

Então: **lote interrompido volta inteiro para a fila, e quem terminar leva o
ticket.** Você perde no máximo os poucos minutos daquele lote. Todos os lotes que
você já fechou continuam seus, e nada mais se perde.

Existe a alternativa de guardar até onde a pessoa varreu e entregar só o resto
para a próxima. Não recomendo, e vale dizer por quê: seria preciso confiar no
ponto de parada declarado, ou provar o pedaço varrido, o que exige parâmetros
próprios para cada fragmento e faz o tamanho do lote variar — e aí o ticket volta
a não valer o mesmo para todo mundo. Toda essa complexidade entraria na parte do
sistema que precisa ser à prova de fraude, para economizar cinco minutos. O
caminho barato é o oposto: manter o lote pequeno o suficiente para que perder um
não doa.

## Rodando com você longe

```bash
puzzlebtc service install    # systemd, launchd ou Windows Service
puzzlebtc service status
puzzlebtc service uninstall
```

O serviço sobe junto com a máquina e continua com a sessão do usuário fechada.
Não impede o computador de dormir: se a máquina dormir, o lote em andamento
expira e volta para a fila, e o serviço pega outro quando ela acordar.

Para deixar rodando de propósito a noite toda, o cliente avisa se a configuração
de energia do sistema vai colocar a máquina para dormir antes.

## A interface

Uma tela só, com o que importa:

```
  puzzlebtc  ·  campanha puzzle #67

  Lote 4a91f2e3          [##############········]  63%
  Restam 2 min 40 s

  Tickets           47
  Nesta sessao       8 lotes  ·  1h 04min
  Velocidade     1,24 Gchaves/s

  Parar: Ctrl-C  (o lote atual volta para a fila)
```

`4a91f2e3` é um identificador opaco, não uma coordenada: o cliente não sabe onde
o lote fica no espaço de busca, e é assim de propósito — ver a seção sobre o lote
cego abaixo.

A mesma informação sai em JSON com `--json`, para quem quiser montar painel
próprio ou acompanhar um rig com várias máquinas.

## Endereço de pagamento

O cliente pede um endereço Bitcoin na primeira execução e o envia junto com a
identidade do participante. É a única informação que o pool precisa dele, e ela
é pública: um endereço não move dinheiro, não deriva chave, e não serve para
nada além de receber.

```bash
puzzlebtc address set bc1q...
puzzlebtc address show
```

Trocar o endereço é permitido a qualquer momento. O coordenador guarda o
histórico, para que um pagamento possa sempre ser rastreado até o endereço que
estava registrado quando a campanha fechou.

Participante sem endereço cadastrado acumula tickets normalmente, mas aparece
marcado no plano de distribuição e o pagamento fica retido até ele informar um.

## O que o cliente nunca faz

Não pede chave privada. Não lê carteira. Não acessa arquivo fora do diretório
dele. Não abre porta de entrada na sua máquina: toda comunicação é ele quem
inicia, para o coordenador.

## O cliente não tem chave nenhuma, nem a do prêmio

O lote não chega como faixa de chaves. Chega como um **ponto da curva** — a chave
pública da primeira chave do lote — e um número de passos. O cliente anda ponto a
ponto, hasheia cada um, e reporta em que posição bateu. Quem soma a posição ao
começo do lote é o coordenador.

Consequências práticas, nas duas direções:

- Não existe chave privada do prêmio na sua memória, nem por um instante. Um
  depurador anexado ao processo, um dump, uma máquina comprometida: nenhum acha
  o que não está lá.
- Você também não consegue conferir sozinho que o terreno recebido está dentro da
  faixa da campanha. Conferir custa um logaritmo discreto por lote, o mesmo que
  atacá-lo custa. A função existe e é pública (`blind.AuditLot`), e serve para
  auditoria por amostragem.
- Você não recebe o número do seu lote. Saber onde ele fica seria quase tão bom
  quanto ter a chave.

Contra cliente modificado isto não é garantia — é o que transforma "ficar com o
que meu computador achou" em "rodar um segundo ataque de propósito". O resto da
defesa é aritmética e está em [`DISSUASAO.md`](DISSUASAO.md).

## O que o cliente não consegue fazer, e é honesto dizer

Impedir que alguém rode uma versão modificada. Fechar o código não resolveria
isso, e custaria a única coisa que permite você confiar no programa: poder
auditá-lo.
