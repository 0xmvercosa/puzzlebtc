# Pesquisa

Levantamento das ferramentas existentes de busca no Bitcoin Puzzle, feito antes
de escrever uma linha do coordenador. Todo número aqui foi **medido ou
verificado neste repositório**, não copiado de README alheio; onde a fonte é a
afirmação do autor da ferramenta, está marcado como tal.

| documento | o que tem |
|---|---|
| [`ferramentas.md`](ferramentas.md) | Análise das quatro ferramentas relevantes, o que cada uma acerta e erra |
| [`benchmarks.md`](benchmarks.md) | Os números medidos, e a aritmética de viabilidade que eles produzem |

## As três conclusões que viraram decisão de projeto

**1. A agregação é a única coisa que torna o problema tratável.** Nenhuma GPU
sozinha resolve o puzzle #71 — nem em mil vidas. Mil delas resolvem em ~4,5 anos.
Isso é o argumento inteiro a favor de um pool, e está em `benchmarks.md`.

**2. Nenhuma das ferramentas existentes sabe onde parou.** `cacagpu` sorteia
pontos aleatórios e varre para sempre sem contabilidade de cobertura;
`CUDACyclone` tem partição determinística e progresso em %, mas nenhuma das duas
tem checkpoint. Um pool precisa saber exatamente qual bloco foi varrido por quem
— daí `internal/keyspace` e a tabela esparsa de blocos.

**3. Nenhuma tem como provar que varreu.** Todas confiam na própria execução,
porque são ferramentas de um usuário só. No momento em que existe rateio de
prêmio, "varri, não achei nada" na palavra do participante vira um caça-níqueis.
Esse buraco é o que `internal/proof` fecha, e é a razão de o projeto existir.
