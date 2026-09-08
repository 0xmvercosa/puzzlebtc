# O cliente modificado

Um participante que apaga o trecho de resgate do cliente fica com o prêmio
inteiro se a chave cair no lote dele. Este documento registra o que foi
investigado contra isso, o que funciona, e o que foi descartado com o motivo.

## Por que não existe detecção técnica barata

**Hash do binário reportado pelo cliente.** O cliente modificado devolve o hash do
original. Uma linha. Toda atestação sem raiz de confiança em hardware pede que a
parte não confiável faça um relatório sobre si mesma.

**Anti-cheat no estilo de jogo.** Funciona lá porque o servidor observa o
resultado: quem mira bem demais gera estatística impossível. Aqui o cliente
modificado produz saída idêntica à do honesto — mesmas testemunhas, mesmas provas
válidas — até o único instante em que importa. Não há sinal comportamental. E
todo DRM e anti-cheat existente já foi quebrado por prêmios muito menores que
este.

**Testar montagem e assinatura sem transmitir.** O modificado assina o teste
honestamente e pula o broadcast só no prêmio. Testa a etapa errada.

**Atestação por TPM.** Funciona de verdade, e exclui Mac, boa parte do Linux,
toda máquina virtual e hardware antigo. Trocaria o risco de roubo pelo pool não
existir.

**Canário de resgate.** Plantar uma chave de um endereço financiado e conferir na
cadeia se as moedas se moveram. É o único mecanismo que funciona, porque a
transmissão é a única etapa que não dá para falsificar. Está implementado em
`internal/coordinator/canary.go` e **desligado por padrão**: exige capital de giro
parado e taxa de transação contínua, e um atacante que consulte o saldo antes de
decidir resgata os canários pequenos e guarda a chave só no prêmio grande.

**Teto de fatia por identidade.** Derrubado por Sybil: a fazenda cria duzentas
identidades de uma placa cada e a fatia total dela não muda. Identidade é grátis.

## Onde o risco realmente está

O participante decide modificar **antes** de achar qualquer coisa. A chance de ele
ser quem encontra é a fatia dele do trabalho do pool, então o ganho esperado do
ataque é fatia × prêmio:

| perfil | fatia do pool | ganho esperado de atacar |
|---|---|---|
| 1 notebook | 0,002% | US$ 11 |
| 1 placa média | 0,02% | US$ 114 |
| 1 RTX 4090 | 0,1% | US$ 568 |
| rig de 6 placas | 0,6% | US$ 3.408 |
| fazenda de 50 placas | 5% | US$ 28.400 |
| fazenda de 200 placas | 20% | US$ 113.600 |

Com prêmio de US$ 568.000 e um pool de mil placas equivalentes.

**A maioria absoluta dos participantes não tem incentivo.** Ninguém constrói um
bypass por onze dólares de expectativa. O risco vive quase inteiramente nas poucas
fazendas grandes.

## O que de fato se faz

**Nada, para a cauda longa.** A aritmética já defende. Gastar dinheiro testando
dez mil participantes cujo incentivo é de dois dígitos seria pagar caro pelo lugar
errado.

**Conhecer os contribuidores grandes.** Uma fazenda de duzentas placas é uma
conversa, não um download anônimo. Saber quem são os poucos que representam fatia
significativa é a defesa que funciona nessa faixa, e não custa nada. É controle
operacional, não técnico, e é assim que isso é tratado na prática.

**Distribuir binário assinado com build reproduzível.** Não impede modificação;
torna rodar o oficial o caminho fácil, que é o que a maioria absoluta faz.

**Registro público de quem tinha cada lote.** Os endereços do puzzle estão entre
os mais observados do Bitcoin. Moeda que se mova sem ninguém reportar identifica
na hora quem estava com aquele terreno, e gastar moeda marcada de um endereço
famoso é um problema real e permanente.

**A fatia de quem encontra.** É uma alavanca, e é fraca. Subir de 50% para 70%
corta o ganho da traição de US$ 261 mil para US$ 148 mil, e custa US$ 113 mil à
plataforma. Atenua, não resolve.

## A defesa que funciona: tornar o roubo inútil

Todas as opções acima tentam **impedir** o roubo, e todas falham. A que funciona
inverte a pergunta: deixa o roubo acontecer e faz com que ele não pague.

Uma chave guardada não vale nada. Para virar dinheiro o ladrão precisa transmitir,
e transmitir publica a chave pública. Dali em diante a privada está num intervalo
conhecido, e o pool a recupera em onze segundos num rig — contra os dez minutos
que a transação dele leva para confirmar.

Somado ao compromisso público de leiloar o prêmio inteiro em taxa antes de deixar
um desertor ficar com ele, o ganho esperado do roubo vai a perto de zero.

Custa quase nada: o kangaroo já é necessário para campanhas de chave exposta, e a
relação com minerador já é requisito do resgate honesto. Não exige capital de giro.

Detalhes, limites e o que ela não alcança: [`DISSUASAO.md`](DISSUASAO.md).

## O risco residual, dito com todas as letras

Uma fazenda grande que modifique o cliente e encontre a chave fica com ela. Não há
mecanismo neste repositório que impeça isso, e o README diz isso ao participante
em vez de fingir garantia.

O que existe é: a maioria não tem motivo, os poucos que têm são conhecíveis, o
caminho honesto é o padrão, a traição é atribuível — e, acima de tudo, ela é
recuperável enquanto o desertor depender da mempool pública.
