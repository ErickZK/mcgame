# auditoria do pipeline do cliente Flash

Data da auditoria: 2026-11-16

Escopo: análise somente leitura do carregamento e da possibilidade de tradução do cliente Flash. Nenhum arquivo do jogo foi sobrescrito e nenhuma reconstrução foi executada.

## resumo

- `frontend/s/sv/GameLoaders.swf` existe e é um SWF `CWS` válido.
- `GameLoaders30.swf` não está presente neste checkout.
- `bin/swfcipher.exe` não está presente neste checkout. O diretório `bin` contém apenas `game.exe`.
- `build-translation-fixed.ps1` e outros scripts de tradução/FFDec não foram encontrados.
- `frontend/s/res/6a41a5ba5c0006ab4e0c1321a2b05027` começa com zlib válido e contém um `CWS`.
- Os arquivos maiores `frontend/s/...` e `frontend/s/sv/...` começam com `BUS`, mas o payload após oito bytes não é aceito nem como zlib padrão nem como DEFLATE bruto.
- `docs/client/config/Language.as` é referência decompilada; alterar esse arquivo sozinho não altera o cliente carregado.
- Os XMLs de `frontend/s/sv/profile` contêm textos vietnamitas diretamente editáveis, mas ainda não foram modificados.

## arquivos e hashes

| arquivo | tamanho | SHA-256 | início hexadecimal |
|---|---:|---|---|
| `frontend/s/6a41a5ba5c0006ab4e0c1321a2b05027` | 16.703.697 | `02FF10ACF66504D1F0B5AE336B34B7668F4B5F628D13F2FCEE82CE2796771807` | `42 55 53 08 43 EC 57 00 78 9D BE BD 06 7E 1B 44` |
| `frontend/s/sv/6a41a5ba5c0006ab4e0c1321a2b05027` | 16.706.401 | `07DA21616D5AC18E2354CD9F2F04AB126FAB85726033E6E861A7499BD064CD38` | `42 55 53 08 A1 00 56 00 78 9D BE BD 06 7E 1B 44` |
| `frontend/s/res/6a41a5ba5c0006ab4e0c1321a2b05027` | 4.049.805 | `DEE1D03129C0B6A8F2DF9C7F9AFF0D55DF8FC29F388E128A2A3A236A7654975D` | `78 DA 00 49 40 B6 BF 43 57 53 09 92 ED E9 00 78` |

Os três arquivos não são cópias idênticas. Os dois maiores compartilham o prefixo `BUS`, mas possuem cabeçalhos diferentes e hashes diferentes.

## evidência do arquivo em `res`

O fluxo de teste foi:

```text
frontend/s/res/<hash>
  -- zlib a partir do offset 0 --> payload de 4.122.409 bytes
  -- assinatura interna --> CWS
  -- zlib do CWS a partir do offset 8 --> corpo de 15.330.698 bytes
```

O cabeçalho `CWS` declara tamanho total `15.330.706` bytes. Portanto, o corpo descompactado esperado é `15.330.698` bytes (`tamanho declarado - 8`), exatamente o tamanho obtido. Isso é evidência estrutural forte de que o arquivo em `res` contém um SWF comprimido válido, embora ainda seja necessário reconstruir o cabeçalho `FWS`/`CWS` antes de entregá-lo ao decompilador.

## evidência dos arquivos `BUS`

O prefixo dos arquivos maiores é:

```text
42 55 53 08 [4 bytes de metadados] 78 9D ...
```

`BUS` é um formato encapsulado, mas `78 9D` não passa a validação de cabeçalho zlib padrão: o campo FCHECK não é válido. O teste também falhou como DEFLATE bruto. Portanto, não é seguro substituir `BUS` por `CWS`/`FWS` nem remover bytes do cabeçalho por tentativa.

O próximo alvo deve ser a rotina do loader que transforma esse payload. Sem `swfcipher.exe` ou o código da rotina, ainda não há evidência suficiente para afirmar o algoritmo, a chave ou o comando de descriptografia.

## loader e ferramentas

O arquivo `GameLoaders.swf` existe com 599.284 bytes, começa com `CWS` e contém a referência textual `?V=`. A busca direta não encontrou o nome completo do recurso hash, provavelmente porque a string é construída, comprimida ou armazenada em bytecode/constantes.

Não foram encontrados:

- `bin/swfcipher.exe`;
- `build-translation-fixed.ps1`;
- scripts com `swfcipher`, `FFDec`, `JPEXS`, `encrypt` ou `decrypt`;
- `GameLoaders30.swf`.

O `bin/game.exe` contém strings genéricas como `encrypt`, `decrypt` e `ZLIB`, mas isso não prova que seja o cifrador do cliente; é um executável do projeto e essas referências podem vir de bibliotecas ou do servidor.

## tradução

`docs/client/config/Language.as` contém arrays de idioma e muitos textos vietnamitas, mas os arquivos de `docs/client` são referências decompiladas. Eles são úteis para mapear chaves e usos, mas não são a entrada executável do cliente.

Os recursos externos abaixo contêm vietnamita e são candidatos a uma tradução sem recompilar o SWF:

- `frontend/s/sv/profile/activity.xml`;
- `frontend/s/sv/profile/guide.xml`;
- `frontend/s/sv/profile/help.xml`.

Uma tradução completa precisará tratar separadamente:

1. strings no SWF principal;
2. strings nos XMLs externos;
3. nomes e descrições enviados pelo servidor/banco de dados;
4. textos desenhados dentro de imagens;
5. fontes incorporadas e suporte a acentos portugueses.

## ferramenta criada para diagnóstico

Foi criado `cmd/bus-extract/main.go`. Ele é somente diagnóstico: preserva a entrada, valida `BUS`, tenta zlib e DEFLATE bruto, grava a saída em caminho separado e identifica assinaturas comuns. O teste passou para o comando, mas falhou corretamente no arquivo `BUS`, confirmando que é necessário descobrir a camada proprietária antes da extração.

Exemplo:

```powershell
go run ./cmd/bus-extract `
  -input ./frontend/s/6a41a5ba5c0006ab4e0c1321a2b05027 `
  -output ./work/payload.bin
```

## proposta de próximos passos

1. Obter o `swfcipher.exe` original ou localizar seu código-fonte/backup.
2. Inspecionar a interface do executável sem executar comandos de escrita.
3. Comparar um SWF original, o arquivo `BUS` correspondente e o resultado de uma descriptografia conhecida.
4. Implementar um leitor `BUS` somente após confirmar o algoritmo e o layout do cabeçalho.
5. Reconstruir o SWF em diretório temporário e validar assinatura, tamanho e decompression streams.
6. Extrair as strings do SWF real e gerar uma tabela de tradução preservando classes, métodos, chaves RPC e identificadores.
7. Traduzir primeiro os XMLs externos, com backup e hashes, pois esse caminho tem menor risco.

Até que o cifrador ou a rotina equivalente seja recuperada, a tradução dos XMLs é a única alteração de baixo risco identificada; a tradução do SWF principal permanece bloqueada pela camada `BUS`.
