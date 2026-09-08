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
  Lote       7 min

  Em 1h  voce fecha    8 lotes
  Em 2h  voce fecha   16 lotes
  Em 6h  voce fecha   48 lotes
  Em 12h voce fecha   97 lotes

  Campanha ativa: puzzle #71
  Valor esperado por lote: US$ 0,00025  (0,0000000031 BTC)
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

O lote é o mesmo para todo mundo, dimensionado para o que um PC comum com placa
de vídeo fecha em torno de dez a quinze minutos. Quem tem mais capacidade não
recebe lote maior: recebe mais lotes, e trabalha vários ao mesmo tempo se tiver
mais de uma GPU.

Isso é o que mantém o ticket com o mesmo significado para todo mundo. Lote maior
para quem tem máquina melhor quebraria a proporcionalidade do rateio, e a
contabilidade perderia o sentido.

Na campanha padrão o lote tem 2³³ chaves, cerca de 8,6 bilhões:

| hardware | tempo por lote | lotes por dia |
|---|---|---|
| CPU 4 núcleos | ~48 min | ~30 |
| GTX 1660 / RTX 3050 | ~13 s | ~6.500 |
| RTX 4060 | ~7 s | ~12.500 |
| RTX 4090 | ~1,4 s | ~62.500 |
| Rig com 6 placas | ~0,2 s | ~375.000 |

O tamanho é escolhido pela CPU: uma máquina comum fecha um lote numa sessão de
trabalho, sem precisar deixar ligado a noite toda para ver o primeiro ticket.

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

## O que o cliente não consegue fazer, e é honesto dizer

Esconder de você a chave do prêmio, se ela cair no seu lote. Ela é calculada na
sua máquina e passa pela sua memória, e o dono da máquina sempre pode lê-la com
um depurador. Fechar o código não resolveria isso, e custaria a única coisa que
permite você confiar no programa: poder auditá-lo.

O que o cliente faz é montar e enviar a transação de resgate em milissegundos,
antes de qualquer interação humana. Contra cliente modificado a defesa não é
técnica, é o registro público de quem tinha o lote.
