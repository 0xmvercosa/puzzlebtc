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

## O compromisso público

O pool se compromete, antecipadamente e por escrito, a **leiloar até o valor
integral do prêmio em taxa de mineração antes de deixar um desertor ficar com
ele.**

Isso é crível justamente por ser ruim para nós: preferimos que o minerador fique
com o dinheiro a premiar a deserção. E é o que zera a conta do ladrão. Ele não
está mais apostando meio milhão contra a chance de ser pego; está apostando meio
milhão contra a certeza de uma guerra de taxa que consome o prêmio inteiro.

O compromisso não custa nada enquanto ninguém desertar.

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
