# Benchmarks

## O que foi medido aqui

Máquina: container Linux, Intel Xeon @ 2.10GHz, 4 vCPUs. Sem GPU — nenhum número
de GPU abaixo foi medido por mim, e está marcado como afirmação do autor.

### Derivação de chave em CPU

Varredura ingênua, uma multiplicação escalar completa por chave — a abordagem do
`btcgo`:

```
55.203 chaves/s por core     220.811 chaves/s em 4 cores
```

### A otimização que separa uma geração da outra

O `btcgoai` traz um benchmark A/B do próprio autor comparando a abordagem antiga
com a nova. Rodei os dois na mesma máquina, 1 core:

```
BenchmarkReferencePerKey        37.646 ns/op   ≈  26.600 chaves/s   ← multiplicação escalar por chave
BenchmarkBatchedIncremental      1.915 ns/op   ≈ 522.200 chaves/s   ← soma incremental + inversão em lote
```

**~20× no A/B interno**, e ~9,5× contra o `btcgo` real medido acima. A diferença:
como as chaves são sequenciais, `P(n+1) = P(n) + G` é uma soma de pontos em vez
de uma multiplicação escalar completa, e uma única inversão de campo é amortizada
por 1024 lanes (truque de Montgomery).

### Verificações de integridade

Confirmadas com o código CPU deste repositório (`internal/btc`):

```
hash160(G) = 751e76e8199196d454941c45d1b3a323f1433bd6      ✅ constante conhecida
hash160s.json × wallets.json do cacagpu: 0 divergências em 161   ✅
10/10 puzzles resolvidos conhecidos (chaves 1,3,7,8,21,49,76,224,467,514)
   → hash160 bate E cai dentro do range declarado          ✅
chave 0x22382FACD0 → 1HBtApAFA9B2YZw3G2YKSMCtb3dVnjuNe2    ✅ puzzle #38, 38 bits
```

A última confirma um acerto real documentado no `CUDACyclone` (encontrado em
~63s no range `2000000000:3FFFFFFFFF`).

## Números de GPU (afirmação dos autores, não medidos aqui)

| ferramenta | GPU | velocidade | fonte |
|---|---|---|---|
| `cacagpu` | RTX 3050 | ~650 Mchaves/s | README + mensagem de commit "600mk/s" |
| `CUDACyclone` | RTX 4060 | 1.238 Mchaves/s | tabela do README |
| `CUDACyclone` | RTX 4090 | 6.214 Mchaves/s | tabela do README (comunidade) |
| `CUDACyclone` | RTX 5090 | 8.408 Mchaves/s | tabela do README (comunidade) |

Normalizando por núcleos×clock, `CUDACyclone` fica ~15% à frente do `cacagpu` —
e parte disso é ganho arquitetural da Ada sobre a Ampere, não do código. **As
duas estão no mesmo patamar algorítmico.** Não compilei nenhuma das duas (sem
GPU no ambiente), então trate a comparação como estimativa.

## A aritmética que decide o projeto

Todas as estimativas em dólar usam **BTC = US$ 80.000**.

### Valor esperado por chave

A chave está em posição uniforme dentro do range, então varrer uma fração F do
espaço dá probabilidade F de encontrá-la. Para um participante que varreu K
chaves de um espaço de tamanho S:

```
E[recebimento] = (K / S) × prêmio × (0,50 + 0,20)
```

Os 0,50 são a chance de ele ser quem acha, proporcional ao trabalho dele; os 0,20
são a fatia dele no bolo de quem ajudou. Simplificando:

```
E[$ por chave varrida] = 0,70 × prêmio ÷ S
```

**Esse número não depende do tamanho do pool.** É contraintuitivo e importa: mais
participantes não aumentam o retorno por chave de ninguém. O que o pool muda é a
frequência com que alguém ganha e o tempo até isso acontecer. O pool converte uma
loteria que você nunca ganharia numa fatia proporcional e regular.

| puzzle | espaço | prêmio (BTC) | prêmio (US$) | $/chave |
|---|---|---|---|---|
| #66 | 2⁶⁵ | 6,6 | 528.000 | 1,00 × 10⁻¹⁴ |
| #67 | 2⁶⁶ | 6,7 | 536.000 | 5,09 × 10⁻¹⁵ |
| #68 | 2⁶⁷ | 6,8 | 544.000 | 2,58 × 10⁻¹⁵ |
| #69 | 2⁶⁸ | 6,9 | 552.000 | 1,31 × 10⁻¹⁵ |
| #71 | 2⁷⁰ | 7,1 | 568.000 | 3,37 × 10⁻¹⁶ |
| #75 | 2⁷⁴ | 7,5 | 600.000 | 2,22 × 10⁻¹⁷ |

Cada puzzle a mais dobra o espaço e o prêmio quase não muda, então o retorno por
chave cai pela metade a cada degrau.

Os valores de prêmio seguem a estrutura conhecida do desafio (puzzles acima do
#65 valendo cerca de N/10 BTC após o aporte de 2017). **Não consegui confirmar
on-chain neste ambiente** — o proxy bloqueia APIs de blockchain. Quem for operar
uma campanha precisa conferir o saldo real do endereço antes de anunciar valor.

### Contra a conta de luz

Energia a US$ 0,15/kWh, consumo estimado por hardware:

| hardware | chaves/s | puzzle #67 | puzzle #69 | puzzle #71 |
|---|---|---|---|---|
| CPU 4 núcleos | 2,1 × 10⁶ | −$0,23 | −$0,23 | −$0,23 |
| RTX 3050 | 6,5 × 10⁸ | −$0,18 | −$0,39 | −$0,45 |
| RTX 4060 | 1,2 × 10⁹ | **+$0,13** | −$0,27 | −$0,38 |
| RTX 4090 | 6,2 × 10⁹ | **+$1,29** | −$0,74 | −$1,26 |
| Rig 6× 4090 | 3,7 × 10¹⁰ | **+$7,74** | −$4,42 | −$7,56 |

Saldo por dia, por máquina. As taxas de GPU são afirmação dos autores das
ferramentas, não medidas aqui; a de CPU é medida.

### Ponto de equilíbrio

| energia | RTX 4090 se paga até |
|---|---|
| US$ 0,15/kWh | puzzle #67 |
| US$ 0,08/kWh | puzzle #68 |
| US$ 0,05/kWh | puzzle #69 |

### O que isso implica para o projeto

**A campanha deve mirar o menor puzzle ainda aberto.** Não é preferência, é a
diferença entre o participante ganhar e perder dinheiro. No #67 uma 4090 paga a
energia e sobra; no #71 ela queima US$ 1,26 por dia.

**CPU nunca se paga.** Serve para testar a instalação e para curiosidade. Vender
CPU como forma de ganhar dinheiro seria desonesto.

**A campanha precisa mostrar a linha de equilíbrio antes da pessoa começar.** O
participante tem que conseguir comparar com a tarifa dele e com o hardware dele
antes de ligar a máquina.

### Tempo, para dimensionar expectativa

Puzzle #67 tem 2⁶⁶ ≈ 7,4 × 10¹⁹ chaves. Fração varrida por ano:

> **Correção.** Uma versão anterior deste documento dizia que 1.000 GPUs médias
> cobriam 0,05% do espaço do #67 por ano. O valor correto é 52,9%: um erro de
> fator mil, que invertia a conclusão. A tabela abaixo está refeita.

Fração do espaço coberta por ano, com GPUs de 1,24 Gchaves/s:

| campanha | 100 placas | 1.000 placas | 10.000 placas |
|---|---|---|---|
| #67 | 5,3% | 52,9% | espaço inteiro em 10 meses |
| #69 | 1,3% | 13,2% | espaço inteiro em 9 meses |
| #71 | 0,3% | 3,3% | 33% |
| #72 | 0,2% | 1,7% | 17% |
| #75 | 0,02% | 0,2% | 2,1% |
| #140 | ~0% | ~0% | ~0% |

Isso muda a leitura do projeto. Um pool de mil placas numa campanha de puzzle
baixo não está comprando bilhete de loteria remoto: está cobrindo metade do
espaço por ano. É a diferença entre "provavelmente nunca" e "provavelmente em
poucos anos".

Nas campanhas altas a conta volta a ser loteria, e cada degrau de puzzle corta a
cobertura pela metade. Por isso a escolha do alvo pesa mais que qualquer outra
variável do projeto.
