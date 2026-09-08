# puzzlebtc

Uma busca coletiva pelas chaves do [Bitcoin Puzzle](https://privatekeys.pw/puzzles/bitcoin-puzzle-tx),
com código aberto e contabilidade aberta.

O espaço de busca é grande demais para qualquer máquina sozinha. A ideia aqui é
juntar as máquinas de várias pessoas, dividir o espaço em lotes, e manter um
registro verificável de quem varreu o quê. Quando alguém encontrar a chave, o
prêmio é dividido entre todos que participaram, na proporção do trabalho de cada
um.

O projeto é aberto em todos os sentidos que importam: o código, o protocolo, o
registro de lotes e o rateio. Você pode rodar, auditar, ou subir o seu próprio
coordenador.

---

## Por que em conjunto

O Bitcoin Puzzle existe desde 2015. Alguém colocou bitcoin em endereços com
chaves de tamanho crescente e deixou lá, de propósito, como medida prática de
quanto o espaço de chaves resiste a força bruta.

Os puzzles pequenos caíram rápido. Os que restam têm espaços de 2⁶⁶ chaves para
cima. Uma RTX 4090 varre cerca de 5×10¹⁴ chaves por dia, o que dá uma fração
minúscula do total. Sozinha, ela pode rodar a vida inteira e não chegar perto.

Mil delas juntas continuam não esgotando o espaço, mas passam a comprar um número
sério de bilhetes. E principalmente: com rateio, cada participante recebe pelo
trabalho que fez, mesmo quando a chave aparece na máquina de outro. É a diferença
entre uma loteria que você quase certamente perde e uma participação
proporcional.

---

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

## Participando

Precisa de uma GPU NVIDIA, ou CPU se for só para experimentar. Roda em Linux,
macOS e Windows.

```bash
# roda dez lotes e sai
puzzlebtc run --blocks 10

# roda até você mandar parar
puzzlebtc run

# instala como serviço, para continuar com o PC ligado e você longe
puzzlebtc service install
```

A interface mostra o lote atual com barra de progresso, seus tickets, e a
velocidade da máquina.

### O rateio

Quando alguém encontra a chave: **50% para quem encontrou, 30% para a
plataforma, 20% dividido entre todos os tickets**. Quem encontrou também tem
tickets e participa dos 20% junto.

Um ticket por lote verificado. Quem varreu mais recebe mais. Não tem mensalidade,
nível, nem vantagem para quem chegou antes.

---

## As contas

**Você não recebe nada a menos que o pool encontre a chave.** Não é rendimento,
não acumula saldo, não tem pagamento periódico. É bilhete de loteria: varrer
compra chance, e a chance só vira dinheiro se a busca do pool acertar antes de
qualquer outra pessoa no mundo. Se um pesquisador solitário ou outro pool achar
primeiro, a campanha acaba e ninguém aqui recebe.

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

Isso muda a lista de alvos bons. Medindo por operações por dólar de prêmio:

| alvo | algoritmo | operações | prêmio | 1.000 placas |
|---|---|---|---|---|
| #67 | força bruta | 7,4 × 10¹⁹ | US$ 536 mil | 5 meses |
| #135 | kangaroo | 3,0 × 10²⁰ | US$ 1,08 mi | 1,5 ano |
| #71 | força bruta | 1,2 × 10²¹ | US$ 568 mil | 6 anos |
| #140 | kangaroo | 1,7 × 10²¹ | US$ 1,12 mi | 8,5 anos |

O #135 paga o dobro do #71 e resolve em um quarto do tempo. O #140 também é
melhor alvo que o #71.

Kangaroo é outro motor de busca, e o cliente atual não faz isso ainda. A análise
completa, incluindo o que precisa ser confirmado na cadeia antes de abrir uma
campanha dessas, está em [`docs/ALVOS.md`](docs/ALVOS.md).

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

| pool | vezes a concorrência inteira | cobertura por ano |
|---|---|---|
| 100 placas | 0,6× | 0,33% |
| 500 placas | 3,2× | 1,65% |
| 1.000 placas | **6,4×** | 3,31% |
| 5.000 placas | **32×** | 16,5% |

Mil placas fazem seis vezes o trabalho de todos os outros buscadores somados.
Não é um mercado saturado: é um espaço quase intocado sendo raspado por um
punhado de máquinas.

Uma ressalva honesta: esse número é o que aquele site enxerga. Quem procura em
silêncio não aparece ali, então trate como piso da concorrência, não como total.

### Quanto o pool consegue cobrir

Aqui está o motivo de existir um pool, e o motivo de chamar mais gente:

| campanha | 100 placas | 1.000 placas | 10.000 placas |
|---|---|---|---|
| puzzle #71 | 0,3% ao ano | 3,3% ao ano | 33% ao ano |
| puzzle #72 | 0,2% ao ano | 1,7% ao ano | 17% ao ano |

Uma placa sozinha cobre 0,0003% do #71 em um ano. Mil placas cobrem 3,3%, o que
já é uma chance de uma em trinta por ano. Dez mil cobrem um terço do espaço
inteiro em doze meses.

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
privada sua. Ele precisa apenas de um endereço para onde o pagamento seria
enviado, que é informação pública. Não existe carteira dentro dele, não existe
seed, não existe nada para roubar da sua máquina. O código é aberto justamente
para você conferir isso antes de instalar, e não acreditar na nossa palavra.

### Onde isso ainda falha

- **Cliente modificado.** Quem alterar o código para não enviar a transação
  consegue ficar com o prêmio. Contra isso: builds reproduzíveis e releases
  assinados, para conferir que o binário bate com o código; e o registro público
  de quem estava com cada lote. Os endereços do puzzle estão entre os mais
  observados do Bitcoin, então moeda que se mova sem ninguém reportar identifica
  na hora quem tinha aquele lote.
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

O projeto precisa de gente em coisas bem diferentes: kernel CUDA, empacotamento
para os três sistemas, interface, e revisão do esquema de verificação. Essa
última em especial: se você encontrar um jeito de passar na verificação sem
varrer o lote, abra uma issue, é a contribuição mais valiosa possível aqui.

A pesquisa que originou o projeto, incluindo a análise das ferramentas existentes
de busca no puzzle, está em [`docs/research/`](docs/research/).

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
