# As ferramentas existentes

Quatro projetos analisados a fundo, do mais antigo ao mais capaz. Todos buscam o
mesmo alvo — os 161 endereços da transação-desafio do Bitcoin Puzzle — e nenhum
resolve o problema que este repositório resolve.

---

## `lmajowka/btcgo` — Go, CPU

83★, 71 forks, **parado desde out/2024**. Projeto comunitário brasileiro do canal
Investidor Internacional; 15 contribuidores, quase tudo via PR.

Pipeline produtor/consumidor com goroutines: uma goroutine gera chaves para um
canal, N workers derivam `hash160` e comparam contra um `map[string]bool`
carregado no boot. Três modos: do início da faixa, sequencial retomando de
`lastkeys.json`, ou random com dedup opcional em BadgerDB.

**O que acerta:** decodifica base58 uma vez no boot e guarda os bytes do hash160,
evitando base58-encode por chave. Separação limpa `core`/`utils`. Uso idiomático
de contexts e canais.

**O que erra:**
- `CreatePublicHash160` faz **multiplicação escalar completa por chave**. É a
  otimização que falta, e vale ~10–20× (medido em `benchmarks.md`).
- `Results.Stop()` e `GenKeys.Stop()` fazem `IsStarted = false` e *depois* testam
  `if IsStarted` — o `CtxCancel()` nunca é chamado. Dead code.
- `App.Modo` é escrito e lido por goroutines diferentes sem sincronização.
- No modo random com DB, grava **uma entrada por chave testada**; o disco estoura
  em minutos a 200k chaves/s.
- `GetStatus()` nunca é chamado: o campo `status` do `ranges.json`, que marca os
  puzzles já resolvidos, é ignorado pelo programa.
- Zero testes, sem CI, sem LICENSE.

---

## `lmajowka/btcgoai` — Go, CPU otimizada

Sucessor direto. **Implementa exatamente a otimização que faltava:**

- `advance()` faz `P(n+1) = P(n) + G` — uma soma de pontos por chave
- **inversão de campo em lote** (Montgomery): uma inversão amortizada sobre 1024 lanes
- trocou `dcrd/secp256k1` por `btcsuite/btcec`, e passou a carregar `hash160s.json` pré-computado
- tem `bench_test.go` e `search_test.go` — o `btcgo` tinha zero

---

## `lmajowka/cacagpu` — CUDA

Só 2 commits (o último se chama literalmente `600mk/s`). Mesma estrutura de
arquivos do `btcgo`, mas com secp256k1 + SHA-256 + RIPEMD-160 inteiros num kernel.

**A matemática está certa e é sofisticada.** Cada thread cobre 513 chaves por
rodada com **uma única inversão de campo**, combinando dois truques:

1. **Inversão em lote.** Passo forward acumula o produto dos 257 denominadores;
   uma inversão; passo backward descasca cada inverso individual.
2. **Simetria ±.** Como `−Q` tem o mesmo `x` que `Q`, os pontos `P+iG` e `P−iG`
   compartilham o denominador `Px − x(iG)`. **Um inverso rende duas chaves** — por
   isso a tabela só precisa de 256 entradas para cobrir 513.

Contei as operações: ~2.580 multiplicações para 513 chaves ≈ **5,0 por chave**,
batendo os "~5,5" que o comentário afirma. As rodadas ladrilham o espaço sem
buraco nem sobreposição, e há um teste (`TestGPUSearchCoversEveryKey`) que prova
isso varrendo todos os 513 offsets mais as emendas — o teste certo, e raro.

O kernel nunca faz aritmética de 256 bits: reporta o match como
`(walk, offset com sinal)` e o host reconstrói `chave = seed[walk] + off`.

**O que erra:**
- **Não tem checkpoint.** Toda execução sorteia 64K seeds novos e zera o contador
  de rodadas. Matou o processo, perdeu tudo. Ironicamente o `btcgo` **tinha** isso.
- **Cobertura sem garantia nem contabilidade.** Seeds aleatórios varrendo pra
  frente, sem dedup e sem partição. Nunca se sabe que fração já foi coberta, e os
  walks passam do máximo do range queimando ciclos fora da faixa.
- **Livelock com progresso falso.** Se o índice do walk vier fora de faixa,
  `gpu_search_run` retorna "não achou" mas deixa `d_found_flag` em 1. Todo thread
  cai no `break` imediato e o kernel não faz nada — enquanto o host continua
  somando `nWalks × rounds × keysPerRound` e imprimindo throughput. Reportaria
  centenas de Mchaves/s checando zero chaves.
- Retornos de `cudaMemcpy` ignorados; 1,9 MB de artefatos de build versionados
  (`.a` e `.o`) que o `make clean` apaga; `go.mod` ainda diz `module btcgoai`;
  README manda rodar `./bench.sh`, que não existe no repo.

---

## `jmr2704/CUDACyclone` — CUDA, multi-GPU

Fork de `Dookoo2/CUDACyclone`, cuja matemática vem do `VanitySearch` do
JeanLucPons. É **o mais maduro dos quatro**.

Mesma família de otimizações (tabela ±G de meio tamanho, inversão em lote), com
lotes de até 1536 chaves. Três decisões que o separam do `cacagpu`:

1. **Partição determinística com contabilidade.** Cada thread carrega uma cota de
   256 bits (`counts256`) que decrementa até zerar; o range é dividido entre GPUs
   e entre threads. Resultado: `Progress: 6.88 %` — ele **sabe** quanto cobriu.
2. **Contagem de trabalho no device.** Acumula hashes por warp e faz um
   `atomicAdd` a cada 65536. Não tem a classe de bug do progresso falso do
   `cacagpu`, porque conta o que realmente aconteceu.
3. **Polling do flag em nível de warp.** Só a lane 0 lê da memória global e
   propaga por `__shfl_sync` — 32× menos tráfego.

**`proof.py` é o melhor artefato dos quatro repositórios.** O upstream teve um bug
real de *key skipping* e escreveu um harness para provar a correção: ambas as
paridades no início e no fim do range, **cobertura completa dos resíduos mod B**
(todas as 512 classes módulo o tamanho do lote), e amostras por quartil. Ataca
exatamente a assinatura do bug — num kernel em lote, chaves puladas aparecem como
classes de resíduo nunca visitadas. Resultado publicado: 848/848.

**O que erra:**
- **Nunca grava a chave encontrada em arquivo.** Os únicos `fprintf` são para
  `stderr` em erros de CUDA. O match vai para `stdout` e só. Uma ferramenta feita
  para rodar semanas que perde o resultado se a janela fechar. O `cacagpu`, com
  todos os defeitos, escreve `found_key_*.txt`.
- **Bug nos exemplos do `--help`** (no commit mais recente, replicado no README):
  `--range 200000000:3FFFFFFFF` é a faixa do puzzle **#34**, mas o endereço
  `1HBtAp...` é o puzzle **#38** — verifiquei, a chave tem 38 bits e não está
  nessa faixa. Quem copiar e colar roda para sempre sem achar nada. Faltou um
  dígito hex em cada ponta; no resto do README o par correto é usado.
- Sem checkpoint, como o `cacagpu`. Mas aqui dá para retomar na mão: o progresso
  em % é exibido e `--range` é parametrizável.

---

## O que nenhuma delas faz

| | cacagpu | CUDACyclone | **puzzlebtc** |
|---|---|---|---|
| Matemática competitiva | ✅ | ✅ | usa as delas |
| Cobertura contabilizada | ❌ | ✅ | ✅ |
| Multi-GPU | ❌ | ✅ | via worker |
| Grava a chave achada | ✅ | ❌ | ✅ |
| Checkpoint / retomada | ❌ | ❌ | ✅ (lease + ticket) |
| **Coordenação entre participantes** | ❌ | ❌ | ✅ |
| **Prova de que varreu** | ❌ | ❌ | ✅ |

As duas últimas linhas são o motivo deste repositório. Todas as quatro são
ferramentas de **um usuário só**: confiam na própria execução porque não há mais
ninguém para enganar. No instante em que existe rateio de prêmio, essa confiança
deixa de ser gratuita — e é isso que `internal/proof` resolve.

**Para usar de verdade:** o worker de referência deste repo é CPU e serve para
definir o protocolo. Para produção, porte o `CUDACyclone` (mais maduro) ou o
`cacagpu` para falar o protocolo em [`../PROTOCOL.md`](../PROTOCOL.md) — são três
comportamentos, e o custo marginal no kernel é zero.
