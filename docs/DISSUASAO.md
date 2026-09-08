# Dissuasão: por que roubar o prêmio não funciona

Este documento descreve a defesa contra um participante que modifica o cliente
para ficar com o prêmio. Ele é público de propósito: **a defesa só funciona se
quem pensa em roubar souber dela antes.**

## O problema, resumido

Nada impede tecnicamente um participante de apagar o trecho de resgate do cliente
e ficar com a chave. A máquina é dele, o cálculo acontece na memória dele, e
verificação de binário é falsificável em uma linha. As alternativas todas foram
investigadas e descartadas em
[`CLIENTE_MODIFICADO.md`](CLIENTE_MODIFICADO.md).

Então esta defesa não tenta impedir o roubo. Ela torna o roubo **inútil**.

## Ficar com o prêmio exige gastá-lo

Uma chave privada não vale nada guardada. Para virar dinheiro, o ladrão precisa
transmitir uma transação gastando as moedas do puzzle.

**E toda transação assinada publica a chave pública na assinatura.**

A partir desse instante a chave privada deixa de ser secreta: ela está num
intervalo conhecido — o intervalo da campanha, que é público — e recuperá-la a
partir da pública custa raiz quadrada do intervalo, não o intervalo inteiro.

| campanha | operações | 1 GPU | rig de 6 |
|---|---|---|---|
| #66 | 1,2 × 10¹⁰ | 12 s | 2 s |
| #71 | 6,9 × 10¹⁰ | 69 s | **11 s** |
| #72 | 9,7 × 10¹⁰ | 2 min | 16 s |
| #75 | 2,8 × 10¹¹ | 5 min | 46 s |

Um bloco leva cerca de 600 segundos. **Numa campanha do #71 o pool recupera a
chave em onze segundos e transmite uma transação concorrente muito antes de a do
ladrão confirmar.**

O algoritmo está em `internal/kangaroo`, com teste que recupera uma chave privada
tendo apenas a pública e o intervalo. Não é descrição, é código que roda.

## O compromisso público: teto de 50%, não de 100%

O pool se compromete, antecipadamente e por escrito, a **cobrir qualquer lance de
um desertor até metade do valor do prêmio.**

Metade, não o total, e o motivo importa. Um compromisso de leiloar o prêmio
inteiro seria um dispositivo do juízo final: dissuadiria só enquanto ninguém o
testasse, e se alguém testasse destruiria exatamente o que deveria proteger. Os
participantes honestos ficariam com zero, junto com o ladrão.

Metade basta, e basta com folga. Não é preciso zerar o ladrão — basta que roubar
renda **menos que ser honesto**:

| fatia do ladrão no pool | ganho sendo honesto | taxa que iguala |
|---|---|---|
| 1% | US$ 285.136 | 50% |
| 10% | US$ 295.360 | 48% |
| 20% | US$ 306.720 | 46% |
| 50% | US$ 340.800 | 40% |

Um teto de 50% cobre todos os perfis. Acima dele, ficar com o prêmio rende menos
do que teria rendido entregá-lo, seja qual for a fatia do desertor.

E o que sobra é distribuído normalmente:

| cenário | taxa paga | participantes recebem | desertor recebe |
|---|---|---|---|
| ninguém deserta | 0,1% | US$ 567.432 | — |
| deserta, recuperamos rápido | 2% | US$ 556.640 | US$ 0 |
| deserta e briga até o teto | 50% | **US$ 284.000** | US$ 0 |

**No pior caso os participantes recebem metade. Não zero.** E o desertor recebe
zero em todos os cenários.

O compromisso não custa nada enquanto ninguém desertar, e o caso provável é o
segundo: recuperação rápida e discreta pelo canal privado, com taxa modesta,
antes de o desertor perceber. A guerra até o teto é o piso da garantia, não o
plano.

## O desertor não está disputando só contra nós

No instante em que ele transmite, a chave pública fica exposta **para todo mundo**.
Não somos os únicos capazes de recuperá-la: qualquer bot de mempool que já observa
endereços de puzzle pode fazer o mesmo, e esse ecossistema existe.

Ou seja, o compromisso do pool **não cria** esse risco para ele. Ele apenas torna
explícito um risco que já existe e que ele provavelmente não calculou.

## A assimetria que decide

O ladrão tem uma saída: não passar pela mempool pública, e submeter direto a um
minerador.

Isso exige relação comercial com um pool de mineração — conta, identificação,
contrato. **Exatamente o que um desertor anônimo não tem, e exatamente o que o
operador do pool tem**, porque precisa disso de qualquer forma para executar o
resgate honesto.

O desertor anônimo só tem a mempool pública. E na mempool pública ele perde.

## Onde esta defesa não alcança

**Um desertor com acesso a submissão privada.** Alguém com relação estabelecida
num pool de mineração transmite sem passar pela mempool e nós nunca vemos a
transação a tempo. Essa pessoa não é um participante anônimo qualquer: é alguém
com operação montada, identificável, e com muito mais a perder que o prêmio.

**Confirmação imediata.** Se a transação do ladrão cair no bloco seguinte antes de
reagirmos, acabou. Com reação em segundos contra um intervalo médio de dez
minutos, isso é cerca de 2% dos casos.

**A guerra de taxa custa aos honestos também.** Se um desertor brigar até o teto,
os participantes recebem metade do que receberiam. É um custo real, e é o preço
de o desertor receber zero. O desenho escolhe metade em vez de nada justamente
para que o pior caso continue pagando quem trabalhou.

**Campanhas de intervalo muito grande.** Acima do #80 a recuperação passa de
quatro minutos num rig, e acima do #100 deixa de caber num bloco. Para campanhas
de puzzle alto por kangaroo, como o #140, esta defesa não existe — mas lá o
ataque da mempool também não existe, pelo mesmo motivo.

## O que isso custa para construir

O observador de mempool são algumas centenas de linhas. O kangaroo já é
necessário para as campanhas de chave pública exposta. A transação concorrente é
trivial. A relação com minerador já é requisito do resgate honesto.

**Custo incremental sobre o que o projeto já precisa: perto de zero. Capital de
giro: nenhum.**

É a única defesa desta análise inteira que não exige o operador adiantar dinheiro
e que continua funcionando contra um participante que já decidiu trair.

## Uma questão em aberto

As moedas do puzzle não são de ninguém: o desafio foi criado para que fiquem com
quem resolvê-lo. Nesse sentido as duas transações estão disputando o mesmo prêmio
sem dono, que é como o desafio funciona desde 2015.

Ainda assim, um operador que pretenda exercer esta recuperação deve tratar disso
com advogado antes, e não no dia. **O valor dissuasivo existe mesmo que ela nunca
seja exercida** — o que importa é o desertor saber, antes de decidir, que ela
está montada.
