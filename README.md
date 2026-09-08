# puzzlebtc

Uma busca coletiva pelas chaves do [Bitcoin Puzzle](https://privatekeys.pw/puzzles/bitcoin-puzzle-tx),
com código aberto e contabilidade aberta.

## Se a chave estiver no seu lote

Na campanha do puzzle #71 o prêmio é de 7,1 BTC, cerca de **US$ 568.000**. Quem
encontrar leva 50%:

### US$ 284.000

Mais a fatia dele no rateio, porque quem acha também tem tickets acumulados.

E se a chave não estiver no seu lote, **você ganha assim mesmo**. Vinte por cento
do prêmio é dividido entre todo mundo que varreu, na proporção do trabalho de
cada um. Quem contribuiu com 1% da busca leva 1% desses 20%, sem precisar ter
sorte nenhuma.

Sozinho você tem uma chance e um resultado: acha e leva tudo, ou não acha e não
leva nada. A segunda opção é quase certa. Aqui você tem as duas: a chance grande
de ser quem encontra, e a certeza de participar do resultado se qualquer um do
grupo encontrar.

E tem uma coisa que joga a favor de quem participa: **o prêmio é em BTC, não em
dólar, e o BTC tende a subir ao longo do tempo.** Os 7,1 BTC do puzzle #71 são os
mesmos 7,1 BTC hoje e daqui a cinco anos, mas o que eles valem em dinheiro
acompanha o preço. Quem varre lote hoje está acumulando participação num prêmio
cuja tendência histórica é valer mais depois. Também pode recuar, e o valor em
dólar acima usa **BTC = US$ 80.000** só para dar escala.

## Por que mais gente aumenta a SUA chance

Isto é o ponto do projeto, e é matemática, não discurso.

Nenhum lote é entregue duas vezes. Quando alguém varre um lote e não acha nada,
aquele terreno sai da conta para sempre, e o espaço que resta encolhe. A chance
do próximo lote é 1 dividido pelo que sobrou. Ou seja:

**Cada lote que qualquer pessoa do pool varre aumenta a chance do seu próximo
lote.**

Quem procura sozinho com sorteio aleatório não tem isso. Ele repete terreno sem
saber, e a chance dele é a mesma no primeiro dia e no milésimo. É o registro de
lotes, com prova de que foram varridos, que transforma trabalho acumulado em
chance crescente.

Por isso trazer gente é do seu interesse direto, e não por generosidade. Mil
pessoas cobrindo terreno melhoram as suas chances a cada hora. Dez mil melhoram
dez vezes mais rápido. E o seu pedaço do bolo não diminui, porque a sua fatia é
proporcional ao seu trabalho, não dividida por cabeça.

| placas no pool | cobertura do #71 por ano | contra toda a concorrência visível |
|---|---|---|
| 100 | 1,7% | 3 vezes |
| 1.000 | 16,6% | 32 vezes |
| 10.000 | espaço inteiro em 7 meses | 320 vezes |
| 50.000 | espaço inteiro em 6 semanas | 1.600 vezes |

Contamos placas, não pessoas. Uma pessoa pode entrar com um notebook ou com um
rig de seis placas, e nada impede que volte com mais depois.

<sub>Cálculo por placa equivalente a uma NVIDIA RTX 4090.</sub>

## Como participar

Três formas, da mais fácil para a mais rápida.

**No navegador, para ver funcionando.** Abre a página e assiste a busca rodar de
verdade na sua máquina, sem instalar nada. Serve para entender o mecanismo e
conferir que o projeto faz o que diz. Não conta como participação: no navegador
um lote levaria semanas, e lote que não fecha não gera ticket. A página avisa
isso na tela.

**Clonando o repositório.** Você lê o código antes de rodar, compila na sua
máquina e sabe exatamente o que está executando. É o caminho recomendado para
quem se importa com isso, e a razão de o projeto ser aberto.

```bash
git clone https://github.com/0xmvercosa/puzzlebtc
cd puzzlebtc && go build ./cmd/worker
./worker --for 6h
```

**Baixando o executável.** Binário pronto para Linux, macOS e Windows, com
assinatura e build reproduzível para você conferir que ele corresponde ao código
publicado.

Em qualquer das três, o esforço é seu. Placa de vídeo rende muito mais que CPU, e
rig rende muito mais que uma placa. Quem só tem CPU também participa: um lote
leva mais tempo, o programa guarda o progresso e continua depois, e o ticket vale
o mesmo.

## Melhorando o algoritmo

O projeto ganha mais com uma otimização boa do que com dez participantes novos.
Uma melhoria de 2 vezes na velocidade equivale a dobrar o pool inteiro, e vale
para todo mundo ao mesmo tempo.

Se você quiser mexer nisso, aqui está o que já sabemos que tem espaço, do mais
promissor para o mais especulativo:

**Endomorfismo GLV.** A curva secp256k1 tem um endomorfismo eficiente que permite
derivar um segundo ponto quase de graça a partir do primeiro. Em varredura isso
pode valer perto de 2 vezes. Ninguém aqui testou ainda.

**Tamanho do lote de inversão.** A inversão de campo em lote é amortizada sobre N
pontos, e o N ótimo depende de registradores e cache da placa. As implementações
de referência usam valores herdados que provavelmente não são ótimos para as
placas atuais.

**O gargalo virou o hash, não a curva.** Com as otimizações de soma incremental e
simetria, a aritmética de curva caiu para cerca de 5 multiplicações por chave,
enquanto SHA-256 mais RIPEMD-160 gastam bem mais que isso. Otimizar hash passou a
render mais que otimizar curva, e é onde quase ninguém olha.

**Motor de kangaroo.** Para os puzzles de chave pública exposta, é o que separa
inatingível de viável. Não existe no projeto ainda, e é a maior contribuição
possível hoje. Ver [`docs/ALVOS.md`](docs/ALVOS.md).

**Ataque à própria verificação.** Se você achar um jeito de passar na verificação
de varredura sem varrer o lote, é a contribuição mais valiosa que existe aqui.
Abra uma issue, mesmo que seja só uma ideia de ataque.

Meça antes e depois, mande o número junto com o código. O repositório tem
benchmarks para comparar, e [`docs/OTIMIZACOES.md`](docs/OTIMIZACOES.md) traz um
levantamento com ganho medido e esforço estimado de cada mudança candidata,
incluindo as que foram medidas e descartadas.

## Como funciona

O coordenador divide o espaço da campanha em lotes de tamanho fixo. Você pede um
lote, ele te entrega um sorteado entre os que ninguém pegou, você varre, e devolve
uma prova de que varreu. Lote verificado vira um ticket no seu nome.

Lote já varrido nunca volta para a fila. Se você desistir no meio, o lote volta a
ficar disponível depois que o prazo expira, e o que já estava confirmado continua
seu.

### A parte difícil: provar que você varreu

Um pool que aceita "varri, não achei nada" na palavra de quem diz distribui
tickets para quem não gastou um watt. Essa é a parte central do projeto, e é onde
está a engenharia.

**Testemunhas.** Uma chave é testemunha quando o HASH160 dela começa com N bits
zero. Testemunhas são raras e não existe atalho para produzir uma: só hasheando
chaves até aparecer. Quem varre o lote encontra elas sem custo nenhum, porque é a
mesma comparação que o programa já faz contra o alvo. Quem tenta inventar a prova
gasta exatamente o que gastaria fazendo o trabalho de verdade.

Contar testemunhas mede quanto do lote foi varrido. Exigir testemunhas em cada
pedaço do lote mede onde. Um buraco de 0,2% já deixa um pedaço vazio e a
submissão é recusada. O coordenador ainda confere uma amostra com matemática de
curva elíptica, sorteada a partir de um segredo dele, então mandar números que só
parecem bem distribuídos também não passa.

**Canários.** O coordenador planta algumas chaves conhecidas dentro do seu lote e
inclui o HASH160 delas na lista que você compara. Um programa que está varrendo
mas não está reportando o que encontra coleta testemunhas normalmente e mesmo
assim não devolve os canários.

As duas verificações custam tempo constante para o coordenador, independente do
tamanho do lote. Especificação completa em [`docs/PROTOCOL.md`](docs/PROTOCOL.md).

---

## Rodando por mais tempo

```bash
puzzlebtc run --for 1h        # roda uma hora e para
puzzlebtc run --for 12h
puzzlebtc run --blocks 20     # roda vinte lotes e para
puzzlebtc service install     # roda com o PC ligado e voce longe
```

A interface mostra o lote atual com barra de progresso, seus tickets e a
velocidade da máquina. Detalhes em [`docs/CLIENT.md`](docs/CLIENT.md).

### O rateio

Quando alguém encontra a chave: **50% para quem encontrou, 30% para a
plataforma, 20% dividido entre todos os tickets**. Quem encontrou também tem
tickets e participa dos 20% junto.

Um ticket por lote verificado. Quem varreu mais recebe mais. Não tem mensalidade,
nível, nem vantagem para quem chegou antes.

---

## As contas

O pagamento só acontece se o pool encontrar a chave. Não é rendimento, não
acumula saldo, não tem pagamento periódico. Se um pesquisador solitário ou outro
pool achar primeiro, a campanha acaba sem pagamento para ninguém daqui.

Com isso claro, dá para dimensionar quanta chance cada máquina compra. Valores
com **BTC = US$ 80.000**; ajuste proporcionalmente se o preço mudar.

### Quanto vale um lote

Um lote são 2³⁹ chaves, cerca de 550 bilhões. A chave está em posição aleatória
no espaço, então cada lote varrido é uma fatia dessa loteria:

```
valor esperado = 0,70 × prêmio × (lotes seus ÷ lotes totais da campanha)
```

O 0,70 é a sua fatia possível: 50% se for você quem acha, mais 20% de rateio
entre quem ajudou.

Campanha do puzzle #71, prêmio de 7,1 BTC (US$ 568.000), com 2 bilhões de lotes
no total:

| sua máquina | lotes por dia | por dia | por mês | por ano |
|---|---|---|---|---|
| GTX 1660 / RTX 3050 | 100 | US$ 0,019 | US$ 0,57 | US$ 6,90 |
| RTX 4060 | 195 | US$ 0,036 | US$ 1,08 | US$ 13,15 |
| RTX 4090 | 975 | US$ 0,181 | US$ 5,42 | US$ 66,00 |
| Rig com 6 placas | 5.860 | US$ 1,085 | US$ 32,55 | US$ 396,00 |

Em BTC, um ano de RTX 4090 nessa campanha vale 0,000825 BTC de valor esperado.

Números pequenos, e é assim mesmo: é o preço de um bilhete numa loteria de meio
milhão de dólares. O que muda a conta não é a sua máquina, é a campanha.

### Escolhendo a campanha

Cada puzzle a mais **dobra** o espaço de busca, e o prêmio quase não muda. O
valor de cada lote cai pela metade a cada degrau:

| campanha | espaço | prêmio | valor de 1 lote | um ano de RTX 4090 |
|---|---|---|---|---|
| puzzle #71 | 2⁷⁰ | 7,1 BTC | US$ 0,00025 | US$ 66,00 |
| puzzle #72 | 2⁷¹ | 7,2 BTC | US$ 0,00013 | US$ 33,46 |

Dentro da força bruta, **a regra é mirar o menor puzzle ainda aberto**. A
diferença entre um degrau e outro é maior que qualquer upgrade de hardware que
você possa comprar.

### Os puzzles de número redondo são outra história

Os puzzles múltiplos de 5 caíram fora de ordem, e não foi sorte. Em 2017 o autor
do desafio gastou desses endereços, e gastar publica a chave pública na
assinatura. Com a chave pública conhecida o problema deixa de ser busca cega:
passa a ser logaritmo discreto num intervalo, que o algoritmo kangaroo resolve em
raiz quadrada do trabalho. No #140 isso é 4 × 10²⁰ vezes menos operações.

Como #67 até #70 e o #135 já foram resolvidos, sobram estes alvos:

| alvo | algoritmo | prêmio | 1.000 placas | 10.000 placas | 50.000 placas |
|---|---|---|---|---|---|
| **#140** | **kangaroo** | **US$ 1,12 mi** | 8,5 anos | **10 meses** | **2,0 meses** |
| #71 | força bruta | US$ 568 mil | 6,0 anos | 7,2 meses | 1,4 mês |
| #72 | força bruta | US$ 576 mil | 12 anos | 1,2 ano | 2,9 meses |

**O #140 é o melhor alvo aberto:** dobro do prêmio por 1,41 vez o trabalho, o que
dá 1,39 vez mais retorno por operação. E é em dez mil placas que ele passa a
resolver dentro de um ano.

<sub>Cálculo por placa equivalente a uma NVIDIA RTX 4090. Operação de kangaroo
assumida com custo equivalente ao de varrer uma chave.</sub>

O plano é lançar no #71, que é o que o motor atual faz, e migrar para o #140
assim que o kangaroo existir. A análise completa está em
[`docs/ALVOS.md`](docs/ALVOS.md).

### Quem mais está procurando

O btcpuzzle.info acompanha a busca declarada no puzzle #71. Em números dele:

| | |
|---|---|
| chaves já varridas | 1,10 × 10¹⁹ |
| fração do espaço | **0,93%** |
| ritmo agregado de todo mundo | 194 Gchaves/s |

Vale reler o último número. **Toda a busca visível no #71 hoje soma 194 bilhões
de chaves por segundo, o equivalente a cerca de 31 placas topo de linha.** Depois
de anos de gente procurando, menos de 1% do espaço foi coberto, e no ritmo atual
eles levariam 193 anos para cobrir o resto.

É contra isso que um pool se compara:

| pool | vezes a concorrência inteira | cobertura do #71 por ano |
|---|---|---|
| 100 placas | 3× | 1,7% |
| 1.000 placas | **32×** | 16,6% |
| 10.000 placas | **320×** | espaço inteiro em 7 meses |

Cem placas já fazem o triplo do trabalho de todos os outros buscadores somados.
Não é um mercado saturado: é um espaço quase intocado sendo raspado por um
punhado de máquinas.

<sub>Cálculo por placa equivalente a uma NVIDIA RTX 4090. Concorrência medida
pelos números públicos do btcpuzzle.info, que só enxerga quem reporta.</sub>

Uma ressalva honesta: esse número é o que aquele site enxerga. Quem procura em
silêncio não aparece ali, então trate como piso da concorrência, não como total.

### Quanto o pool consegue cobrir

Aqui está o motivo de existir um pool, e o motivo de chamar mais gente:

| campanha | 100 placas | 1.000 placas | 10.000 placas |
|---|---|---|---|
| puzzle #71 | 0,3% ao ano | 3,3% ao ano | 33% ao ano |
| puzzle #72 | 0,2% ao ano | 1,7% ao ano | 17% ao ano |

Uma placa sozinha cobre 0,0017% do #71 em um ano. Mil placas cobrem 16,6%. Dez
mil cobrem o espaço inteiro em sete meses.

E tem uma coisa que só um pool com registro de lotes consegue oferecer: **cada
lote fechado aumenta a chance do próximo.** Como nenhum lote é entregue duas
vezes, o espaço que sobra encolhe, e a chance do lote seguinte é 1 dividido pelo
que resta. Quem busca sozinho com sorteio aleatório repete terreno sem saber e
fica com a mesma chance para sempre. É a garantia de não repetir que transforma
trabalho acumulado em chance crescente.

E é por isso que trazer gente é do seu interesse direto: **o seu valor por lote
não muda com o tamanho do pool**, porque a sua fatia é proporcional ao seu
trabalho. O que muda é a probabilidade de o prêmio sair para dentro do pool em
vez de para um concorrente de fora. Mais gente aqui não divide o seu bolo, e
aumenta a chance de existir bolo.

### Progresso

O painel mostra quantos lotes o pool já fechou, que fração do espaço isso
representa, e quantos participantes estão ativos. Todo lote fechado tem prova
verificada por trás, então esse número é auditável, ao contrário de qualquer
alegação de varredura por aí.

A fração vai começar próxima de zero e subir devagar. É informação honesta, não
barra de progresso de instalador: o valor está em cada lote ser uma chance real,
não em chegar a cem por cento.

Metodologia e medições em [`docs/research/benchmarks.md`](docs/research/benchmarks.md).

## Como o prêmio é resgatado

O maior risco de um projeto assim é quem encontra a chave sumir com ela. Não dá
para impedir por criptografia: quem varre o lote calcula a chave na própria
máquina e nenhum protocolo tira ela de lá.

O que dá para fazer é encurtar a janela até quase zero. Ao encontrar a chave, e
antes de mostrar qualquer coisa na tela, o cliente monta a transação de resgate,
assina, e envia. O endereço de destino está no código deste repositório: qualquer
pessoa confere para onde o dinheiro vai antes de instalar. A distribuição sai
dali conforme o rateio publicado, com ledger aberto.

### O ataque da mempool, e por que ele decide o desenho

Transmitir a transação de resgate pela rede normal perde o prêmio, e o motivo é
específico deste tipo de endereço.

A assinatura de uma transação **expõe a chave pública** de quem assinou. Com a
chave pública conhecida e o intervalo do puzzle sendo de 2⁶⁶, o algoritmo
kangaroo do Pollard recupera a chave privada em cerca de 2³³ operações, o que é
questão de segundos numa GPU. Quem estiver observando a mempool vê a transação,
extrai a pública, recupera a privada e transmite uma concorrente com taxa maior
para o próprio endereço. A janela é de um bloco, cerca de dez minutos, e pagar
taxa alta não resolve: o atacante sempre pode pagar mais. Já aconteceu com
soluções de puzzle antes.

Por isso o resgate **não passa pela mempool pública**. A transação vai por
submissão direta a minerador, por canais privados que aceitam transação fora da
propagação normal, e o cliente nunca faz broadcast aberto. Isso é requisito do
protocolo de resgate, não otimização.

### O cliente nunca toca nas suas chaves

O programa que você roda não pede, não lê, não armazena e não transmite chave
privada sua. A única coisa que ele precisa de você é **um endereço Bitcoin para
receber**, que é informação pública e não move dinheiro nenhum. Não existe
carteira dentro dele, não existe seed, não existe nada para roubar da sua
máquina. O código é aberto justamente para você conferir isso antes de instalar,
em vez de acreditar na nossa palavra.

### E a chave do prêmio, se for você quem achar?

Ela é enviada ao operador, que executa a distribuição. Vale ser direto sobre uma
coisa: **não temos como impedir que você veja essa chave.** Ela é calculada na
sua máquina, passa pela sua memória, e quem controla a máquina consegue lê-la
com um depurador. Isso não é limitação nossa, é como computador funciona, e
fechar o código não mudaria nada além de tirar de você a possibilidade de
auditar o programa.

O que existe é o resgate automático descrito acima, que fecha essa janela em
milissegundos no cliente oficial, e o registro público de qual participante
tinha o lote. Não é garantia matemática, e o README não vai fingir que é.

### Onde isso ainda falha

- **Cliente modificado.** Quem alterar o código para não enviar a transação
  tentaria ficar com o prêmio. Contra isso existe o **canário de resgate**: o
  pool planta, em alguns lotes, uma chave de um endereço com bitcoin de verdade.
  O cliente que recebe esse lote é obrigado a executar o resgate inteiro sobre
  dinheiro que está realmente lá, e o pool confere na cadeia. Não apareceu
  transação, o cliente está modificado, e o participante perde acesso aos lotes
  antes de ter chance de encontrar qualquer coisa. Ver
  [`docs/CANARIO.md`](docs/CANARIO.md).
- **A distribuição depende de quem opera.** O prêmio chega a um endereço
  controlado pelo operador do pool, que executa o rateio. O ledger de tickets é
  aberto para você conferir quanto lhe cabe, mas o pagamento em si depende do
  operador cumprir o combinado. Isso está dito também na seção final, e é a
  principal coisa em que você precisa confiar para participar.

Nenhuma das duas é garantia matemática. São incentivo e rastreabilidade, e é
honesto chamar do que são.

---

## Estado do projeto

O coordenador funciona: divisão do keyspace, entrega aleatória de lotes,
verificação de varredura, tickets e rateio. Os testes cobrem varredura honesta
aceita, varredura parcial recusada por buraco de cobertura, testemunhas forjadas
e duplicadas recusadas, cliente que não reporta recusado, prêmio falso recusado,
lote expirado e lote de outra pessoa não resgatáveis, e rateio fechando exatamente
no valor do prêmio.

Falta, em ordem:

1. **Identidade do participante.** Hoje o nome é auto-declarado, então qualquer um
   credita ticket em qualquer nome, o que anula toda a verificação. É o primeiro
   item e é bloqueante.
2. **Resgate automático.** O caminho de submissão direta a minerador está
   desenhado, não construído. É o item mais delicado do projeto.
3. **Cliente de GPU.** O worker atual é CPU e existe para definir o protocolo sem
   ambiguidade.
4. **Interface e medição de hardware.** Barra de progresso do lote, escolha de
   quanto tempo rodar, e serviço em background. Desenho em
   [`docs/CLIENT.md`](docs/CLIENT.md).
5. **Pagamento.** Nada aqui movimenta satoshi ainda.

## Contribuindo

Além do algoritmo, o projeto precisa de gente em kernel CUDA, empacotamento para
os três sistemas, e interface. A pesquisa que originou tudo isto, com a análise
das ferramentas de busca que já existem, está em
[`docs/research/`](docs/research/).

---

## Antes de participar

**Isto é uma loteria.** A chance de achar é proporcional à fração do espaço que o
pool varre, e ela começa perto de zero. As tabelas são valor esperado de longo
prazo, não previsão. Você pode participar por um ano e não receber nada.

**A plataforma é confiável por escolha, não por criptografia.** Ela guarda o
segredo dos canários, distribui os lotes e mantém o ledger. O ledger é aberto
para você conferir os seus tickets, mas o pagamento depende dela cumprir o
combinado.

**Situação legal.** Distribuir prêmio entre participantes pode ser enquadrado
como loteria, jogo, ou oferta de valores mobiliários dependendo do país. Quem
operar precisa resolver isso com advogado antes de aceitar participantes. Isto
não é orientação jurídica.

**Sobre a busca em si.** O Bitcoin Puzzle foi criado para ser quebrado, os
endereços são públicos e o dinheiro foi colocado lá como desafio aberto. Isso é
diferente de atacar endereço de terceiro, que não tem nada de legítimo e não é o
que este projeto faz nem apoia.
