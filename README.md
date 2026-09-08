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

Vale saber no que você está entrando antes de ligar a máquina. Os números abaixo
usam **BTC = US$ 80.000**; ajuste proporcionalmente se o preço mudar.

A chave está em posição aleatória dentro do espaço, então varrer uma fração dele
dá exatamente essa fração de chance de achar:

```
retorno esperado = 0,70 × prêmio × (suas chaves ÷ tamanho do espaço)
```

O 0,70 é a sua fatia possível: 50% se for você quem acha, mais 20% de rateio.

Campanha do puzzle #67, prêmio de aproximadamente 6,7 BTC:

| hardware | chaves por dia | retorno esperado/dia |
|---|---|---|
| CPU 4 núcleos | 1,8 × 10¹¹ | US$ 0,001 |
| RTX 4060 | 1,1 × 10¹⁴ | US$ 0,54 |
| RTX 4090 | 5,4 × 10¹⁴ | US$ 2,73 |
| Rig 6× 4090 | 3,2 × 10¹⁵ | US$ 16,38 |

Retorno esperado é média de longo prazo, não pagamento periódico. Você acumula
participação e recebe quando o pool encontrar. Pode passar um ano sem receber
nada.

**A escolha da campanha é o que mais pesa.** Cada puzzle a mais dobra o espaço e o
prêmio quase não muda, então o retorno por chave cai pela metade a cada degrau.
Uma RTX 4090 rende US$ 2,73/dia no #67, US$ 0,70 no #69 e US$ 0,18 no #71. Por
isso o pool mira sempre o menor puzzle ainda aberto, e a campanha ativa fica
visível antes de você instalar qualquer coisa.

Em CPU o retorno é desprezível em qualquer campanha. Ela serve para testar a
instalação e para acompanhar o projeto, não como forma de ganhar dinheiro.

O seu retorno por chave não muda com o tamanho do pool, porque a sua fatia é
proporcional ao seu trabalho. Mais participantes aumentam a frequência com que o
pool encontra alguma coisa e diminuem o tempo de espera, mas não dividem o seu
bolo.

Metodologia e as medições em [`docs/research/benchmarks.md`](docs/research/benchmarks.md).

---

## Como o prêmio é resgatado

O maior risco de um projeto assim é quem encontra a chave sumir com ela. Não dá
para impedir por criptografia: quem varre o lote calcula a chave na própria
máquina e nenhum protocolo tira ela de lá.

O que dá para fazer é encurtar a janela até quase zero. Ao encontrar a chave, e
antes de mostrar qualquer coisa na tela, o cliente monta uma transação levando o
prêmio para um endereço multisig 2-de-3 publicado neste repositório, assina,
transmite para vários nós, e só então reporta ao coordenador. São milissegundos
entre achar e travar. O endereço de resgate está no código, então qualquer pessoa
confere para onde o dinheiro vai antes de instalar. A distribuição sai do
multisig conforme as regras publicadas, com ledger aberto.

Onde isso ainda falha, e vale dizer com todas as letras:

- **Cliente modificado.** Quem alterar o código para não transmitir consegue
  ficar com tudo. Contra isso: builds reproduzíveis e releases assinados, para
  conferir que o binário bate com o código; e o registro público de quem estava
  com cada lote. Os endereços do puzzle estão entre os mais observados do
  Bitcoin, então moeda que se mova sem ninguém reportar identifica na hora quem
  tinha aquele lote.
- **Os donos do multisig podem conluiar.** Por isso 2-de-3 com uma parte
  independente, regras publicadas antes de qualquer campanha começar, e ledger
  aberto.

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
2. **Resgate automático.** O mecanismo do multisig está desenhado, não construído.
3. **Cliente de GPU.** O worker atual é CPU e existe para definir o protocolo sem
   ambiguidade.
4. **Interface.** Barra de progresso, escolha de lotes, serviço em background.
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
