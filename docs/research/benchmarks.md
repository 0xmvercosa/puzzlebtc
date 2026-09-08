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

## A aritmética de viabilidade

Puzzle #71 tem 2^70 ≈ 1,18×10²¹ chaves.

| | chaves/s | #71 sozinho |
|---|---|---|
| CPU, 1 core | 55 mil | 680 milhões de anos |
| RTX 3050 | 650 milhões | 57 mil anos |
| RTX 5090 | 8,4 bilhões | **4.460 anos** |

E é aqui que o pool deixa de ser conveniência e vira necessidade:

| GPUs (classe 5090) | tempo esperado para #71 |
|---|---|
| 1 | 4.460 anos |
| 100 | 45 anos |
| 1.000 | **4,5 anos** |
| 10.000 | **163 dias** |

Nenhum participante individual tem chance. Dez mil deles, juntos, têm.

**O que isso não muda:** a probabilidade de sucesso em qualquer janela é
proporcional à fração do keyspace varrida, e essa fração começa indistinguível de
zero. Ninguém deve entrar esperando retorno. O campo `fraction_swept` da API
devolve o número honesto, não um teatro de progresso.
