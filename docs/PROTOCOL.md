# Protocolo puzzlepool v1

Contrato entre coordenador e worker. Um worker de GPU que respeite este documento
é intercambiável com o worker de referência em `cmd/worker`.

## Ciclo

```
POST /v1/lease     → recebe o lote + parâmetros + watchlist
   (varre o lote)
POST /v1/submit    → entrega a prova, recebe o ticket
GET  /v1/progress  → estado da campanha
GET  /v1/attest/root → compromisso assinado sobre os leases emitidos
GET  /healthz
```

## 0. Campanha cega

Numa campanha cega — o padrão — o worker **não recebe chaves nem o índice do
bloco**. Recebe o ponto da curva onde o lote começa e quantas chaves ele cobre, e
endereça o lote pelo `lease_token`.

Isso não é detalhe de apresentação: quem conhece o índice do bloco recupera o
começo do lote com um logaritmo discreto sobre a largura do deslocamento em vez
de sobre a campanha inteira — no #71, 2¹⁷ operações em vez de 2³⁶. Por isso o
índice não aparece no lease, nem no recibo, nem no `ticket_id`. Ver
[`../internal/blind`](../internal/blind) e [`DISSUASAO.md`](DISSUASAO.md).

Um coordenador rodando com `-unblinded` devolve o formato antigo, com
`block_index`, `lo_key_hex` e `hi_key_hex`. É modo de depuração: serve para
apontar um worker novo a um bloco conhecido e conferir a saída. Não é como uma
campanha real roda.

## 1. Alugar um lote

```http
POST /v1/lease
{"worker_id": "alice", "count": 50}
```

`count` maior que 1 devolve `{"leases": [...]}` em vez de um lease solto. Sem
`count`, ou com `count` 1, a resposta continua sendo o objeto plano de sempre.

**Peça em bloco se você tem GPU.** O lote é dimensionado para uma CPU fechar em
cerca de uma hora, o que numa placa topo de linha dá 1,4 segundo. Uma requisição
por lote seriam 62 mil conexões por dia só da sua máquina. Peça 50 ou
100 de uma vez, trabalhe todos, e submeta um por um. O limite por requisição é
200.

```json
{
  "campaign_id": "puzzle-71",
  "lot": {
    "start_point": "03eccc6186a70229506a747a8400347a33d2dee07a4986f162da813623984bab37",
    "length": "8589934592"
  },
  "length": "8589934592",
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

`start_point` é a chave pública comprimida (33 bytes, hex) da primeira chave do
lote. `503 no_block_available` significa que a campanha se esgotou — faça backoff,
não desista. Os offsets são sempre relativos ao começo do lote, então cabem em
`uint64` por mais fundo que ele esteja no keyspace.

## 2. Varrer

Parta de `P = start_point` e ande `P += G` uma vez por chave. Para cada `offset`
de `0` a `length - 1`:

1. Calcule `h = RIPEMD160(SHA256(serializa_comprimido(P)))`.
2. Se `h` tem **≥ `witness_bits` bits zero à esquerda**, registre `offset` como
   testemunha.
3. Se `h` está na `watchlist`, registre `offset` em `canaries`.
4. Só numa campanha `-unblinded`: se `h` é igual ao alvo, registre `offset` como
   `found_offset`.

Numa campanha cega o passo 4 não existe, e é de propósito: **o worker reporta todo
hit da watchlist do mesmo jeito e não distingue o prêmio de um canário.** O
coordenador re-deriva os hits que não plantou e reconhece o alvo do lado dele. Um
worker que reporte tudo uniformemente nunca perde um achado por configuração
errada.

Somar `G` é o mesmo laço que `cacagpu` e `CUDACyclone` já rodam — inversão em lote
de Montgomery amortiza a aritmética de curva para cerca de 5 multiplicações por
chave. Derivar cada chave do zero seria mais lento, não mais rápido.

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
| `500` | token de lease desconhecido, ou apresentado por outro worker |
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

## 4. Se o seu lote contiver a chave

Você reporta o offset em `canaries`, como faz com qualquer hit da watchlist, e
pronto. **O coordenador re-deriva todo hit que ele não plantou e compara com o
alvo**, então é lá que o achado é reconhecido. Custa uma operação de curva e só
roda nos extras, então o caso comum não paga nada.

Você não fica com a chave porque nunca a teve: o que passou pela sua máquina foram
pontos, e o começo do lote — o outro termo da soma — está só no coordenador.

Nenhum protocolo pode obrigar quem acha a reportar, e nenhum aqui tenta. O que
este faz é tornar o não-reportar inútil sem um segundo ataque, e caro mesmo com
ele. Ver [`DISSUASAO.md`](DISSUASAO.md) e o modelo de confiança no README.

## Parâmetros

Vêm no lease e valem para aquele bloco. Não os embuta no worker.

| campo | significado | default (lotes 2³³) |
|---|---|---|
| `witness_bits` | bits zero à esquerda que definem uma testemunha | 21 (~4.096/lote) |
| `buckets` | fatias para o teste de cobertura | 128 (~32 testemunhas cada) |
| `canaries` | quantos canários voltar | 4 |
| `sample_size` | quantas testemunhas o servidor verifica | 64 |
| `sigmas` | desvios abaixo da média que definem os pisos | 5 |

`witness_bits = block_bits - 12` mira ~4.096 testemunhas por lote, o que dá cerca
de 32 KB por submissão. Os buckets caem junto com as testemunhas, e isso não é
opcional: manter 512 buckets com 4.096 testemunhas põe a média em 8 por bucket,
o piso de Poisson desaba para 1, e **cerca de 17% das submissões honestas passam
a ser recusadas por engano.**

O lote padrão é de 2³³ chaves, cerca de 8,6 bilhões: aproximadamente uma hora
numa CPU de quatro núcleos, 13 segundos numa placa de entrada e 1,4 segundo numa
topo de linha. Ele é dimensionado pela CPU de propósito, para que uma máquina
comum consiga fechar um lote inteiro numa sessão. Quem tem mais capacidade pega
mais lotes, nunca lotes maiores, para o ticket continuar valendo o mesmo trabalho
para todo mundo.

## 5. A raiz de atestação

```http
GET /v1/attest/root
```

```json
{
  "campaign_id": "puzzle-71",
  "root": "60ca1a25…",
  "leaves": 148302,
  "published_at": 1789000000,
  "public_key": "1772e33a…",
  "signature": "d679ce85…"
}
```

Compromisso assinado sobre **todos os leases já emitidos**, sem revelar nenhum.
É servido aberto de propósito: um compromisso que só o operador pudesse ver
depois do fato não seria compromisso. Arquive a resposta com data — é ela que
transforma, mais tarde, "este participante estava com este terreno" em algo
conferível por qualquer um.

`404 attestation_disabled` significa que a campanha roda sem chave de atestação,
e portanto sem atribuição. Ver [`DISSUASAO.md`](DISSUASAO.md).

## Limites

Índice de bloco e offset são `uint64`. Com no máximo 2^63 blocos de 2^63 chaves,
o alcance é 2^126 chaves: **puzzles até #126 são endereçáveis, #127 em diante
não**. Puzzle #71 usa 2^30 blocos — folga enorme. Campanha grande demais é
rejeitada na criação, nunca truncada.
