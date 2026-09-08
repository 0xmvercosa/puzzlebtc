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

## Comparando alvos pelo que interessa

A métrica útil é operações por dólar de prêmio: quanto trabalho custa cada dólar
que se pode ganhar. Menor é melhor.

| alvo | algoritmo | operações | prêmio | ops por dólar |
|---|---|---|---|---|
| #67 | força bruta | 7,4 × 10¹⁹ | US$ 536 mil | 1,4 × 10¹⁴ |
| #135 | kangaroo | 3,0 × 10²⁰ | US$ 1,08 mi | 2,7 × 10¹⁴ |
| #140 | kangaroo | 1,7 × 10²¹ | US$ 1,12 mi | 1,5 × 10¹⁵ |
| #71 | força bruta | 1,2 × 10²¹ | US$ 568 mil | 2,1 × 10¹⁵ |
| #72 | força bruta | 2,4 × 10²¹ | US$ 576 mil | 4,1 × 10¹⁵ |
| #145 | kangaroo | 9,4 × 10²¹ | US$ 1,16 mi | 8,1 × 10¹⁵ |

Tempo até resolver, com operação de kangaroo custando aproximadamente o mesmo que
uma chave varrida:

| alvo | 1.000 placas | 10.000 placas |
|---|---|---|
| #67 força bruta | 5 meses | 2 semanas |
| #135 kangaroo | 1,5 ano | 2 meses |
| #71 força bruta | 6 anos | 7 meses |
| #140 kangaroo | 8,5 anos | 10 meses |
| #145 kangaroo | 48 anos | 5 anos |

**#135 é o melhor alvo de puzzle alto por uma margem larga**, e é competitivo com
os puzzles baixos de força bruta enquanto paga o dobro. **#140 é melhor que #71.**

## O que precisa ser confirmado antes de escolher

Nada disso vale se as premissas estiverem erradas, e duas não foram verificadas:

**Quais chaves públicas estão realmente expostas.** O ambiente onde este
documento foi escrito não tem acesso a API de blockchain, então a exposição não
foi confirmada endereço por endereço. Antes de abrir uma campanha de kangaroo é
obrigatório verificar na cadeia que aquele endereço tem transação de saída e
extrair a chave pública dela. Um alvo sem pubkey exposta é força bruta pura, e a
tabela acima não se aplica a ele.

**Quais puzzles seguem em aberto.** Da mesma forma, o saldo de cada endereço
precisa ser conferido na cadeia. A estrutura de prêmio usada aqui (puzzle n
valendo cerca de n/10 BTC) segue o que se sabe do desafio, e também precisa ser
confirmada.

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

Abrir a primeira campanha em **força bruta no menor puzzle ainda aberto**. É o
que o código já faz, é onde o custo por dólar é menor, e permite lançar sem
construir um segundo motor.

Tratar **#135 por kangaroo** como a segunda campanha, e planejá-la desde já: é o
melhor alvo de prêmio alto por margem confortável, e o coordenador já tem quase
tudo de que ela precisa.

Não abrir campanha em puzzle alto sem pubkey exposta. Sem o atalho do kangaroo
esses alvos são inatingíveis por qualquer quantidade de hardware, e anunciá-los
seria vender bilhete sem sorteio.
