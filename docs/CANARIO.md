# Canário de resgate

Mecanismo para descobrir que um cliente foi modificado **antes** de ele ter a
chance de roubar um prêmio, e não depois.

## O problema

O caminho de resgate de um cliente honesto roda **uma vez na vida dele**, no dia
em que encontra a chave. Até lá, um cliente com esse trecho apagado é
indistinguível de um honesto: ele varre, entrega provas válidas, acumula tickets
normalmente. Não há nada para observar.

Verificação de binário não resolve. Checksum reportado pelo próprio cliente é
falsificado numa linha. Assinatura de código prova o que publicamos, não o que
está rodando na máquina dele. Atestação por hardware funcionaria, mas exclui a
maioria absoluta das máquinas de consumidor.

E banir depois do roubo é inútil: quem levou meio milhão de dólares não se importa
em perder acesso ao pool.

## O mecanismo

O operador deriva uma chave dentro de um lote, **financia o endereço dela com
bitcoin de verdade**, e planta. Quando um participante recebe esse lote, o
cliente dele encontra a chave e é obrigado a executar o caminho de resgate
inteiro: montar a transação, assinar, transmitir. Sobre dinheiro que está
realmente lá.

O operador então observa a cadeia.

**Não apareceu transação, o cliente está modificado.** O participante perde acesso
aos lotes e volta a procurar sozinho contra o keyspace inteiro, sem a coordenação
que garante que nenhum terreno se repete. Que é exatamente o objetivo: a exclusão
acontece antes do roubo.

Isso transforma um caminho de código que rodaria uma vez na vida em algo
exercitado rotineiramente, **verificado na cadeia** — a única evidência que
cliente nenhum consegue forjar.

## Como funciona na prática

```bash
# 1. o coordenador deriva o canario para um lote ainda nao alugado
puzzlebtc canary plan --block 8412337
   bloco    8412337
   offset   198
   endereco 12RQE2yvySxvvquVH4uxepmmvF6ezMQ7RH
   chave    0000...a3f1     <- guarde fora do coordenador

# 2. o operador financia esse endereco da carteira dele

# 3. registra o financiamento
puzzlebtc canary arm --block 8412337 --sat 50000 --txid abc123...

# 4. o coordenador entrega esse lote no lugar de um sorteado, na taxa configurada
# 5. o auditor confere a cadeia depois do prazo e bane quem nao resgatou
```

A chave privada do canário **nunca é gravada no coordenador**. Ela é derivada na
hora de financiar e pode ser derivada de novo a partir do lote e do segredo da
campanha. Um coordenador que guardasse essas chaves colocaria o dinheiro de todos
os canários a uma invasão de distância.

O canário de resgate reusa o último offset dos canários de reporte, então o
cliente não consegue distinguir os dois olhando a watchlist.

## Salvaguardas contra banir gente honesta

Um banimento é uma ação pesada tomada sobre evidência automática, então o
mecanismo erra de propósito para o lado de não banir:

**Prazo generoso.** Vinte e quatro horas contadas do aluguel, contra um lote que
leva menos de uma hora. Notebook fechado no meio, lote deixado expirar, minerador
lento: nada disso pode ser confundido com cliente modificado.

**Falha do observador nunca vira banimento.** Se o nó do operador estiver fora, o
canário fica em aberto e é reavaliado depois. Sem isso, toda queda de
infraestrutura expulsaria participantes honestos.

**Sem observador configurado, ninguém é banido.** Numa operação piloto sem acesso
a cadeia, canários continuam sendo plantados e entregues, e simplesmente não são
julgados. Não se bane sobre evidência que ninguém leu.

**A acusação é estreita.** O banimento afirma apenas que esta identidade recebeu
um lote com moedas gastáveis, foi instruída a resgatá-las, e não o fez. Software
honesto faz isso todas as vezes.

## O que ele não pega

Um atacante que consulte o saldo do UTXO antes de decidir. Ele resgata os
canários pequenos e guarda a chave só quando o valor for grande.

Mas isso deixa de ser apagar uma linha e passa a ser construir um desvio ciente
de valor, que precisa funcionar de primeira e nunca errar, porque qualquer falha
é banimento. A barreira sai de trivial para trabalho de engenharia deliberado,
com risco de perder o acesso a cada lote recebido.

Duas coisas compõem com isso e valem mais que qualquer refinamento do canário:

**O operador varrer parte do espaço no hardware dele.** Se ele tem 30% da força
do pool, em 30% das vezes quem encontra é ele, e nesses casos o risco é zero. É a
única defesa que não depende de confiar em ninguém.

**Financiar alguns canários com valores maiores.** Quanto mais alto o valor que o
atacante precisa deixar passar para não se revelar, mais caro fica esperar pelo
prêmio grande.

## Custo

Cada canário custa uma transação de financiamento mais o valor deixado nele. Com
`canary_rate` em 1%, um participante que faz cem lotes encontra em média um
canário. A taxa é configurável e é uma troca direta: dinheiro por velocidade de
detecção.
