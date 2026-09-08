# O participante que modifica o cliente

Este documento é público de propósito. A defesa só funciona se quem pensa em
roubar souber dela antes de decidir.

Todos os valores usam **BTC = US$ 80.000**. O prêmio do puzzle #71 são 7,1 BTC,
ou **US$ 568.000**.

## O problema

Nada impede tecnicamente um participante de apagar o trecho de resgate do
cliente e ficar com o prêmio. A máquina é dele, o cálculo acontece na memória
dele, e conferência de binário é falsificável em uma linha.

Tentar **detectar** a modificação não funciona, e as tentativas estão descartadas
uma a uma, com motivo, em [`CLIENTE_MODIFICADO.md`](CLIENTE_MODIFICADO.md). Um
cliente modificado é indistinguível de um honesto até o único instante em que
acha a chave — e nesse instante já é tarde.

Então a defesa não é detecção. São três coisas somadas, e nenhuma delas depende
de confiar no participante.

## 1. Ele não recebe chave nenhuma

O lote não é entregue como faixa de chaves. É entregue como ponto da curva:

```
A = a*G     chave pública da primeira chave privada do lote
n           quantas chaves o lote cobre
```

O cliente anda `A`, `A+G`, `A+2G`, …, hasheia cada ponto, e reporta o offset em
que bateu. Quem tem `a` é o coordenador, e é ele quem calcula `a + offset`.

O que roda na máquina do participante são pontos. Ponto não é chave.

Para transformar ponto em chave é preciso resolver um logaritmo discreto sobre a
faixa da campanha — cerca de `2*sqrt(largura)` operações:

| campanha | custo do log discreto | comparado a varrer um lote¹ |
|---|---|---|
| #71 | 6,9 × 10¹⁰ | 8× |
| #75 | 2,8 × 10¹¹ | 32× |
| #80 | 1,5 × 10¹² | 181× |
| #135 | 3,0 × 10²⁰ | mais que o pool inteiro fará na vida |

<sub>¹ lote padrão de 2^33 chaves, cerca de uma hora de CPU.</sub>

**Isso não é um muro no #71.** São 6,9 × 10¹⁰ operações, menos de um minuto numa
GPU boa. O que muda é outra coisa:

- **O cliente honesto não vaza o que não tem.** Dump de memória, máquina
  comprometida, participante curioso lendo o código: nenhum produz chave. A
  promessa de que o participante não vê a chave deixa de ser política e passa a
  ser propriedade do protocolo.
- **O roubo deixa de ser passivo.** "Só fiquei com o que meu computador achou"
  sai de cena. Extrair a chave exige rodar deliberadamente um segundo ataque,
  que não vem no cliente, sobre dado que o pool entregou cego.
- **Em campanha de faixa grande vira muro de verdade**, porque o custo cresce com
  a raiz da faixa.

O código é `internal/blind`. A versão ingênua disso era quebrada — se os lotes
ladrilhassem a faixa a partir de `Min`, todo início de lote cairia no reticulado
`Min + j*2^blockBits` e sairia por baby-step/giant-step em milissegundos. O
ladrilhamento é deslocado por um segredo da campanha justamente por isso, e os
dois ataques rodam nos testes em vez de serem descritos.

Pelo mesmo motivo o participante **não recebe o índice do seu bloco**: saber o
índice reduziria o log discreto da largura da campanha para a largura do
deslocamento — no #71, de 2^36 para 2^17. O índice não aparece no lease, nem no
recibo, nem no `ticket_id`.

Em troca, uma coisa honesta a dizer: **o participante não consegue mais conferir
sozinho que o terreno que recebeu está dentro da faixa da campanha.** Conferir
custa exatamente o que atacar custa — um log discreto por lote —, o que cabe para
um auditor conferindo alguns lotes e não cabe para quem quer roubar. A função é
`blind.AuditLot`, é o mesmo kangaroo dos dois lados, e é pública.

## 2. A conta que decide, e ela não depende de ameaça nenhuma

Chame de **q** a probabilidade de o desertor conseguir de fato converter o roubo
em dinheiro que ele mantém.

Para um participante que varre `X` chaves de uma faixa de `R`:

```
sendo honesto:  (X/R) * P * [ 0,50 + 0,20*(1-m) ]
desertando:     (X/R) * P * [    q + 0,20*(1-m) ]
```

O segundo termo é idêntico nos dois casos — o desertor continua no pool, continua
entregando os lotes que não contêm a chave, e continua recebendo a fatia de
ajudante como qualquer um. Ele se cancela. Sobra:

> **Desertar só compensa se q > 50%.**

Não depende do tamanho do desertor, não depende de quantos desertores existem, e
não depende de o pool prometer nada. Depende de um número só: os 50% de quem
encontra.

E é uma alavanca do desenho, não uma fatalidade:

| fatia de quem encontra | roubo compensa se |
|---|---|
| 50% (atual) | q > 50% |
| 60% | q > 60% |
| 70% | q > 70% |

Subir a fatia do descobridor sobe a barra na mesma proporção. O custo é sair da
fatia da plataforma ou da dos ajudantes. É uma escolha aberta.

## 3. Por que q é baixo, e não somos nós que fazemos isso

Chave privada guardada não vale nada. Para virar dinheiro é preciso transmitir
uma transação — **e toda transação assinada publica a chave pública na
assinatura.**

A partir desse instante a chave privada está num intervalo conhecido e público, e
recuperá-la custa a raiz do intervalo: **onze segundos para o #71 num rig de seis
GPUs**, contra um intervalo médio de dez minutos entre blocos. O código está em
`internal/kangaroo`, com teste que recupera a chave privada tendo só a pública e
o intervalo.

O desertor tem três saídas, e nenhuma boa:

**Mempool pública.** Ele expõe a chave pública para todo mundo ao mesmo tempo.
Não somos os únicos capazes de recuperá-la — qualquer bot que já observa endereços
de puzzle faz o mesmo, e esse ecossistema existe desde antes deste projeto. Aqui q
é aproximadamente a chance de o bloco seguinte sair antes de qualquer um reagir:
algo como 2%.

**Relay privado.** Ele manda a transação direto para uma empresa. Essa empresa
recebe a transação assinada, que contém a chave pública. Ela pode recuperar a
chave e levar os US$ 568.000 sozinha, e as moedas do puzzle não têm dono para
processar ninguém. Ou seja: q vira a probabilidade de uma empresa desconhecida
recusar meio milhão de dólares de graça.

**Minerar o próprio bloco.** Essa funciona, e q ≈ 1. Exige ser minerador com
hashrate relevante — alguém com operação montada, identificável, e com muito mais
a perder que o prêmio.

Nada disso é ameaça nossa. É a estrutura do problema, e provavelmente é a parte
que quem pensa em desertar não calculou.

Se acontecer, o pool compete: recupera a chave da pública exposta e transmite a
transação concorrente para o endereço da campanha, e **distribui normalmente, 50 /
30 / 20, para quem trabalhou.** Não é um dispositivo de destruição, não é leilão
até o teto, não é guerra de taxa combinada de antemão: é a recuperação que
qualquer um faria, e ela domina não fazer nada — alguma coisa para os honestos é
melhor que zero.

## 4. Ele fica com o nome nisso

O coordenador assina o lease antes de o lote ser varrido. Esse registro diz, com
carimbo de tempo anterior ao roubo: **este participante estava com este terreno.**

Quando uma chave do puzzle aparece na blockchain, `Campaign.BlockIndexOf` diz em
qual lote ela estava, e o lease diz quem estava com o lote. Não é suspeita, é
correspondência aritmética verificável por qualquer um, publicada antes do crime.

Um projeto aberto com lista pública de participantes e endereço de pagamento em
arquivo transforma um roubo anônimo em um roubo assinado. As moedas ficam
marcadas desde o primeiro bloco.

## O que um desertor custa a quem é honesto

Esta é a parte que costuma ser sentida errado. O medo é "basta um cara modificar
e o pool inteiro perde". A conta diz outra coisa.

Um desertor só pode levar a chave se a chave estiver **no terreno que ele mesmo
varreu**. A probabilidade disso é a fatia dele do trabalho do pool. Um desertor
com 1% do hashrate leva 1% do prêmio em valor esperado. Não 100%.

E a fatia de quem encontra não depende do comportamento dos outros. Só o bolo dos
20% está em risco, e só na proporção do desertor:

| fração do pool que é desertora | o honesto recebe, comparado a um pool sem nenhum |
|---|---|
| 1% | 99,7% |
| 10% | 97,1% |
| 25% | 92,9% |
| **50%** | **85,7%** |

**Um pool que fosse metade desertores ainda pagaria a um participante honesto
86% do que pagaria um pool limpo.** Um desertor não desmonta o pool; ele tira
dele, no máximo, o tamanho do próprio trabalho.

## O que o pool troca por isso, dito com todas as letras

Por chave varrida, o valor esperado de participar é **0,7×** o de buscar sozinho —
os 30% da plataforma. Ninguém entra num pool porque o valor esperado por chave
sobe; ele desce.

O que sobe é a chance de receber alguma coisa. Sozinho, um participante com um
milésimo do hashrate do pool tem um milésimo da chance de ver dinheiro na vida.
No pool ele recebe sempre que **qualquer um** encontrar. É a mesma troca de todo
pool de mineração desde 2010: menos valor esperado por unidade de trabalho, ordens
de grandeza mais chance de o trabalho virar pagamento.

## Onde esta defesa não alcança

**Um desertor que é minerador.** Ele transmite pelo próprio bloco, q ≈ 1, e a
conta acima recomenda que ele deserte. Contra ele sobram o lote cego (que o obriga
a rodar o log discreto primeiro) e a atribuição (que o nomeia). Nada mais.

**Confirmação imediata.** Se a transação do ladrão cair no bloco seguinte antes de
qualquer reação, acabou. Com reação em segundos contra dez minutos de média, isso
é cerca de 2% dos casos.

**Campanhas acima do #100.** A recuperação por kangaroo deixa de caber num
intervalo de bloco, e o item 3 desaparece. Em compensação o item 1 vira muro: no
#135 o log discreto que o desertor precisaria rodar é maior que a campanha
inteira.

**O log discreto no #71 é barato.** Menos de um minuto de GPU. O lote cego não
impede quem já decidiu roubar e sabe o que está fazendo — ele impede o roubo
casual, torna o deliberado inegável, e não custa nada a quem é honesto.

Nenhuma das quatro peças torna o roubo impossível. Somadas, elas fazem com que
roubar renda menos que colaborar para qualquer participante que não seja
minerador, custe ao pool honesto no máximo o tamanho do desertor, e deixe o nome
dele no registro.
