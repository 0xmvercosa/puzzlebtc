# Escolha de alvo

Qual puzzle uma campanha ataca é a decisão que mais pesa neste projeto. Pesa mais
que hardware, mais que tamanho do pool, mais que qualquer otimização de código.

## Duas famílias de alvo, dois algoritmos

Os puzzles se dividem em dois grupos, e a diferença entre eles é de vinte ordens
de grandeza.

**Chave pública desconhecida.** O endereço nunca gastou nada, então só se conhece
o HASH160. Não há atalho: é preciso derivar chave por chave e comparar. Custo:
2ⁿ⁻¹ operações para o puzzle n. É o que este repositório faz hoje.

**Chave pública exposta.** Em 2017 o autor do desafio movimentou fundos, e gastar
de um endereço publica a chave pública na assinatura. Com ela conhecida, o
problema deixa de ser busca cega e vira logaritmo discreto num intervalo, que o
algoritmo kangaroo de Pollard resolve em cerca de 2·√(2ⁿ⁻¹) operações de grupo.

É por isso que os puzzles múltiplos de 5 caíram fora de ordem enquanto os
vizinhos seguiam abertos. Não foi sorte nem força bruta: foi outro algoritmo,
disponível só para eles.

| puzzle | força bruta | kangaroo | ganho |
|---|---|---|---|
| #120 | 6,7 × 10³⁵ | 1,6 × 10¹⁸ | 4 × 10¹⁷ vezes |
| #135 | 2,2 × 10⁴⁰ | 3,0 × 10²⁰ | 7 × 10¹⁹ vezes |
| #140 | 7,0 × 10⁴¹ | 1,7 × 10²¹ | 4 × 10²⁰ vezes |
| #150 | 7,1 × 10⁴⁴ | 5,3 × 10²² | 1 × 10²² vezes |

## Quais alvos continuam abertos

Os puzzles #67, #68, #69, #70 e #135 já foram resolvidos. Isso deixa:

- **Força bruta:** o menor aberto é o **#71**.
- **Kangaroo:** o menor aberto é o **#140**, já que o #135 caiu.

## Comparando os alvos abertos

A métrica útil é operações por dólar de prêmio: quanto trabalho custa cada dólar
que se pode ganhar. Menor é melhor.

| alvo | algoritmo | operações | prêmio | ops por dólar |
|---|---|---|---|---|
| **#140** | **kangaroo** | 1,67 × 10²¹ | **US$ 1,12 mi** | **1,49 × 10¹⁵** |
| #71 | força bruta | 1,18 × 10²¹ | US$ 568 mil | 2,08 × 10¹⁵ |
| #72 | força bruta | 2,36 × 10²¹ | US$ 576 mil | 4,10 × 10¹⁵ |
| #73 | força bruta | 4,72 × 10²¹ | US$ 584 mil | 8,09 × 10¹⁵ |
| #145 | kangaroo | 9,44 × 10²¹ | US$ 1,16 mi | 8,14 × 10¹⁵ |
| #150 | kangaroo | 5,34 × 10²² | US$ 1,20 mi | 4,45 × 10¹⁶ |

### Tempo até resolver, por tamanho de pool

Placas topo de linha, equivalentes a uma RTX 4090 a 6,2 Gchaves/s:

| alvo | 1.000 placas | 10.000 placas | 50.000 placas |
|---|---|---|---|
| **#140 kangaroo** | 8,5 anos | **10 meses** | **2,0 meses** |
| #71 força bruta | 6,0 anos | 7,2 meses | 1,4 mês |
| #72 força bruta | 12 anos | 1,2 ano | 2,9 meses |
| #73 força bruta | 24 anos | 2,4 anos | 5,8 meses |
| #145 kangaroo | 48 anos | 4,8 anos | 11,6 meses |
| #150 kangaroo | 273 anos | 27 anos | 5,5 anos |

Um pool real não é feito só de placa topo de linha. Com placas intermediárias,
equivalentes a uma RTX 4060 a 1,24 Gchaves/s, os mesmos alvos ficam cinco vezes
mais lentos:

| alvo | 1.000 placas | 10.000 placas | 50.000 placas |
|---|---|---|---|
| **#140 kangaroo** | 43 anos | 4,3 anos | **10 meses** |
| #71 força bruta | 30 anos | 3,0 anos | 7,3 meses |
| #72 força bruta | 60 anos | 6,0 anos | 1,2 ano |
| #145 kangaroo | 242 anos | 24 anos | 4,8 anos |

A leitura prática: **abaixo de mil placas nenhum alvo aberto sai em tempo
razoável.** A escala em que o projeto começa a fazer sentido é dez mil placas, e
é aí que #140 e #71 passam a resolver dentro de um ano.

**O #140 é o melhor alvo aberto.** Paga o dobro do #71 por 1,41 vez o trabalho, o
que dá 1,39 vez mais retorno por operação. Em números absolutos:

| | #140 kangaroo | #71 força bruta |
|---|---|---|
| prêmio | US$ 1.120.000 | US$ 568.000 |
| operações | 1,67 × 10²¹ | 1,18 × 10²¹ |
| 1.000 placas | 8,5 anos | 6,0 anos |
| 5.000 placas | 1,7 ano | 1,2 ano |
| 10.000 placas | 10 meses | 7 meses |

A vantagem de 1,39 vez é real mas não é esmagadora, e o #140 custa um motor de
busca inteiro que ainda não existe. A diferença que pesa mais na prática pode ser
outra: **o prêmio anunciado é o dobro**, e isso importa para atrair participante
muito além do que a razão matemática sugere.

## O que precisa ser confirmado antes de escolher

Nada disso vale se as premissas estiverem erradas, e duas não foram verificadas:

**Quais chaves públicas estão realmente expostas.** O ambiente onde este
documento foi escrito não tem acesso a API de blockchain, então a exposição não
foi confirmada endereço por endereço. Antes de abrir uma campanha de kangaroo é
obrigatório verificar na cadeia que aquele endereço tem transação de saída e
extrair a chave pública dela. Um alvo sem pubkey exposta é força bruta pura, e a
tabela acima não se aplica a ele.

**Quais puzzles seguem em aberto.** Da mesma forma, o saldo de cada endereço
precisa ser conferido na cadeia antes de abrir campanha. A lista de resolvidos
usada aqui (todos até #70, mais os múltiplos de 5 até #135) veio de informação
repassada, não de verificação na cadeia feita neste repositório.

**O custo real de uma operação de kangaroo.** As tabelas de tempo assumem que ela
custa aproximadamente o mesmo que derivar e comparar uma chave. É uma
aproximação razoável — as duas fazem aritmética de curva e um teste barato — mas
não foi medida. Um fator de 2 ou 3 aqui muda todos os prazos.

## O que muda no projeto para atacar um alvo de kangaroo

Não é trocar um parâmetro. É outro motor.

**A busca não é por intervalo.** Kangaroo não varre faixas contíguas: solta
caminhadas pseudoaleatórias que param em pontos distintos, e resolve quando duas
caminhadas colidem. Não existe "este intervalo está varrido".

**A prova de trabalho muda de forma, mas não de ideia.** O esquema de testemunhas
deste repositório verifica que um intervalo foi coberto. Em kangaroo o análogo é o
ponto distinto: um ponto cuja coordenada x tem N bits zero à esquerda. São raros
pela mesma razão, não têm atalho pela mesma razão, e o coordenador verifica cada
um em tempo constante pela mesma razão. A ideia central sobrevive; a
implementação é outra.

**O argumento das chances crescentes não sobrevive.** Em varredura sem reposição,
cada lote fechado aumenta a chance do próximo, porque o espaço restante encolhe.
Kangaroo é busca de colisão probabilística: não há espaço encolhendo, e a chance
por unidade de trabalho é constante. Uma campanha de kangaroo é honesta, mas
precisa ser explicada de outro jeito.

**O que dá para reaproveitar:** aluguel de trabalho, prazo, tickets, rateio,
verificação em tempo constante, resgate. Praticamente todo o coordenador. O que
precisa ser escrito é o motor de busca e o formato da prova.

## Recomendação

**Lançar no #71 por força bruta. Construir o kangaroo em seguida e migrar para o
#140.**

O motivo de não começar direto no #140, apesar de ele ser o melhor alvo, é que
ele custa um motor de busca inteiro antes do primeiro participante entrar. O #71
usa o que já está pronto e testado, permite validar coordenação, verificação,
tickets e resgate com gente real, e custa apenas 1,39 vez mais trabalho por dólar
enquanto isso acontece.

Assim que o kangaroo existir, o #140 passa a ser o alvo principal: dobro do
prêmio, melhor retorno por operação, e um argumento de recrutamento mais forte.

Duas coisas que **não** devem ser feitas:

**Não abrir campanha em puzzle alto sem confirmar que a chave pública está
exposta.** Sem o atalho do kangaroo esses alvos são inatingíveis por qualquer
quantidade de hardware, e anunciá-los seria vender bilhete sem sorteio.

**Não anunciar prazo.** As tabelas de tempo acima assumem que a operação de
kangaroo custa aproximadamente o mesmo que uma chave varrida, e isso não foi
medido. Também assumem que ninguém de fora acha primeiro. São dimensionamento
interno, não promessa.
