# Protocolo puzzlepool v1

Contrato entre coordenador e worker. Um worker de GPU que respeite este documento
é intercambiável com o worker de referência em `cmd/worker`.

## Ciclo

```
POST /v1/lease     → recebe bloco + parâmetros + watchlist
   (varre o bloco)
POST /v1/submit    → entrega a prova, recebe o ticket
GET  /v1/progress  → estado da campanha
GET  /healthz
```

## 1. Alugar um bloco

```http
POST /v1/lease
{"worker_id": "alice"}
```

```json
{
  "campaign_id": "puzzle-71",
  "block_index": 733412891,
  "lo_key_hex": "400000000000000000",
  "hi_key_hex": "40000000000000ffff",
  "length": "1099511627776",
  "lease_token": "9f2c…",
  "expires_at": 1789000000,
  "proof_params": {
    "witness_bits": 26,
    "buckets": 512,
    "canaries": 4,
    "sample_size": 64,
    "sigmas": 5
  },
  "watchlist": ["b190e2…", "0f31ab…", "…"]
}
```

`503 no_block_available` significa que a campanha se esgotou — faça backoff, não
desista. Os offsets são sempre relativos a `lo_key_hex`, então cabem em `uint64`
por mais fundo que o bloco esteja no keyspace.

## 2. Varrer

Para cada chave `k` em `[lo, hi]`, com `offset = k - lo`:

1. Calcule `h = RIPEMD160(SHA256(pubkey_comprimida(k)))`.
2. Se `h` tem **≥ `witness_bits` bits zero à esquerda**, registre `offset` como
   testemunha.
3. Se `h` está na `watchlist`, registre `offset` como hit.
4. Se `h` é igual ao alvo da campanha, registre `offset` como `found_offset`.

Três exigências que derrubam a maioria das implementações na primeira tentativa:

- **Testemunhas vão estritamente crescentes.** Isso não é convenção, é a defesa
  contra duplicatas — a forma mais barata de inflar a contagem. Se o kernel emite
  fora de ordem, **ordene antes de enviar**; o coordenador rejeita como
  `witnesses_unordered`.
- **Bits zero à esquerda, não bytes.** `witness_bits: 26` significa 26 bits, não
  3 bytes e um pouco.
- **Contagem baixa reprova mesmo com tudo certo.** O limiar é 5σ abaixo da média
  de Poisson. Não pule o último pedaço do bloco.

O teste do passo 2 é a mesma comparação que o kernel já faz contra o alvo — em
`cacagpu` e `CUDACyclone` é mudar a constante do prefixo. O custo marginal é zero.

## 3. Submeter

```http
POST /v1/submit
{
  "worker_id": "alice",
  "campaign_id": "puzzle-71",
  "block_index": 733412891,
  "lease_token": "9f2c…",
  "witnesses": [1204, 88301, 175002, …],
  "canaries": [412887, 690122, …],
  "found_offset": null
}
```

`200` com o recibo, ou:

| HTTP | quando |
|---|---|
| `400` | JSON malformado, campo desconhecido, `worker_id` ausente |
| `409 lease_invalid` | lease expirou, é de outro worker, ou o bloco já foi liquidado |
| `422` | a requisição estava bem-formada, a **prova** não |
| `503 no_block_available` | campanha esgotada (só em `/lease`) |

Códigos `422`, todos com detalhe legível:

| código | o que aconteceu |
|---|---|
| `witnesses_unordered` | não estritamente crescentes (ou duplicadas) |
| `witness_out_of_range` | offset além do fim do bloco |
| `too_few_witnesses` | contagem abaixo do piso de 5σ |
| `coverage_gap` | um bucket ficou vazio — aquele trecho não foi varrido |
| `canary_missed` | um canário plantado não voltou |
| `witness_forged` | uma testemunha amostrada não bate a dificuldade |
| `found_mismatch` | o `found_offset` não hasheia para o alvo |
| `found_out_of_range` | `found_offset` além do fim do bloco |

Prova rejeitada **não consome o lease**: um worker com bug corrigível pode
reenviar até o lease expirar.

## 4. Se você achar a chave

Mande em `found_offset`. Se o worker estiver configurado com o alvo errado e
reportar o acerto apenas como mais um hit da watchlist, **o coordenador o
recupera mesmo assim** — ele re-deriva qualquer hit que não seja canário plantado
e compara com o alvo. Custa uma operação de curva e só roda nos extras, então o
caso comum não paga nada.

Nenhum protocolo pode obrigar quem acha a reportar. Ver o modelo de confiança no
README.

## Parâmetros

Vêm no lease e valem para aquele bloco. Não os embuta no worker.

| campo | significado | default (blocos 2^40) |
|---|---|---|
| `witness_bits` | bits zero à esquerda que definem uma testemunha | 26 (~16.384/bloco) |
| `buckets` | fatias para o teste de cobertura | 512 (~32 testemunhas cada) |
| `canaries` | quantos canários voltar | 4 |
| `sample_size` | quantas testemunhas o servidor verifica | 64 |
| `sigmas` | desvios abaixo da média que definem os pisos | 5 |

`witness_bits = block_bits - 14` mira ~16.384 testemunhas por bloco: resolução
estatística boa e ~128 KB por submissão.

## Limites

Índice de bloco e offset são `uint64`. Com no máximo 2^63 blocos de 2^63 chaves,
o alcance é 2^126 chaves: **puzzles até #126 são endereçáveis, #127 em diante
não**. Puzzle #71 usa 2^30 blocos — folga enorme. Campanha grande demais é
rejeitada na criação, nunca truncada.
