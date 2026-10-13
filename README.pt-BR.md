# Serena Terminal

O Serena Terminal não é um emulador de terminal e não tem relação com a ferramenta de programação "Serena", da Oraios.

Ele é o `sereno`, um único comando, sem dependências, que faz o terminal que você já usa acompanhar o modo escuro, claro e de alto contraste do sistema, com os seis esquemas de cores do [Serena Theme](https://github.com/joaooliveira10/serena-theme) e com o contraste de cada cor medido. No Windows, ele acrescenta ao Windows Terminal um perfil "Serena" e seis esquemas de cores e prepara o PowerShell 5.1 e o 7; no macOS e no Linux, prepara o bash e o zsh e grava arquivos de tema para Ghostty, kitty, WezTerm e Alacritty. Antes de gravar qualquer coisa ele mostra um plano, guarda o registro de cada arquivo que grava, e o `sereno uninstall` remove exatamente esses arquivos.

Read in English: [README.md](README.md).

## Situação

<!-- sereno:status unreleased -->
**A versão 0.1 ainda não foi lançada.** Não existe a tag `v0.1.0` nem release dela. As tags da forma `v0.1.0-rc.N` são candidatas a release: existem para ensaiar o próprio processo de release, e o que se compila a partir de uma delas não é a versão 0.1. Os arquivos de release e os scripts de instalação descritos abaixo só podem vir, por enquanto, de uma candidata, quando houver uma publicada na página de releases como pré-lançamento; fora isso, o programa é compilado a partir de uma cópia deste repositório.

O que já foi conferido, e o que não foi:

- **Rodou e passou** no Windows 11 com Windows PowerShell 5.1 e Git Bash: os testes em Go, os testes de comportamento dos arquivos de sessão do PowerShell e do bash, uma execução do programa compilado numa pasta pessoal temporária (apply, status, theme, check, export, linhas de hook, uninstall, com a pasta inteira comparada antes e depois) e os testes dos scripts de instalação.
- **Rodou e passou** no Linux (Ubuntu no WSL): os testes em Go, os testes do bash e os testes do script de instalação no `dash`.
- **Rodou e passou só no CI** (runners hospedados pelo GitHub: Windows Server 2025, macOS 26 em arm64, Ubuntu 24.04; isso vale para a execução 38457983362 do workflow de CI, em 2026-10-13, que rodou o commit 515fb8f): os mesmos testes e, com eles, tudo o que não existe na máquina do mantenedor: o PowerShell 7 (7.6.6 no Windows e no Linux, 7.6.5 no macOS, com PSReadLine 2.4.5), o zsh 5.9 no macOS e no Linux e o bash 3.2.57 no macOS. Ninguém viu nada disso numa tela.
- **Entrou depois desse commit:** frases destes documentos, a linha de ajuda de `--inband`, a nota que o `sereno uninstall` põe no backup de um arquivo de inicialização que não está mais lá, e testes. Isso rodou só na máquina do mantenedor (Windows 11, Windows PowerShell 5.1), até a próxima execução do CI. Todos os jobs da execução 38457983362 passaram, e com ela o que tinha mudado desde as execuções de 2026-10-08 também rodou no CI: a linha de hook do PowerShell, o que o `init.ps1` faz numa janela de console do Windows cujo fundo não é preto e numa sessão sem console, quando o PowerShell é consultado sobre o perfil dele, e que ele é consultado sem a variável `PSExecutionPolicyPreference`, a pasta inicial do perfil Serena, quando o `sereno status` avisa sobre o tema do Windows Terminal, e algumas frases do apply, do status e do uninstall. Como toda execução do CI, ela foi de pastas pessoais temporárias em runners hospedados: não diz nada sobre um perfil de usuário de verdade.
- **Visto numa tela, uma coisa até agora:** em 2026-10-08 o mantenedor rodou `sereno theme --paint light` numa aba aberta do Windows Terminal 1.24, na máquina dele, e a aba ficou clara. Enviar a paleta a uma aba aberta é aquilo em que seguir o sistema se apoia.
- **Ainda não foi feito:** o programa não foi aplicado a um perfil de usuário de verdade, então o perfil Serena, os seis esquemas de cores e uma sessão que segue o sistema sozinha ainda não foram vistos no Windows Terminal; esse teste de aceitação está previsto para antes do lançamento. Os quatro formatos de tema exportados não foram conferidos nos emuladores.

As capturas de tela entram em `docs/images` depois desse teste de aceitação; por enquanto não há nenhuma.

## O que ele grava, e onde

O `sereno apply --dry-run` mostra o plano e não altera nada. Esta é a saída dele num Windows 11 com Windows Terminal, numa execução real em uma pasta pessoal de teste (essa pasta aparece como a pasta pessoal de um usuário chamado `user`; `dev` é a versão de um programa que não veio de um arquivo de release):

```text
sereno dev: plan
  mkdir   C:\Users\user\AppData\Local\sereno
  mkdir   C:\Users\user\AppData\Local\sereno\shell
  mkdir   C:\Users\user\AppData\Local\Microsoft\Windows Terminal\Fragments\SerenaTerminal
  create  C:\Users\user\AppData\Local\sereno\manifest.json
  create  C:\Users\user\AppData\Local\sereno\shell\init.ps1
  create  C:\Users\user\AppData\Local\sereno\shell\init.bash
  create  C:\Users\user\AppData\Local\sereno\shell\init.zsh
  create  C:\Users\user\AppData\Local\sereno\mode
  create  C:\Users\user\AppData\Local\Microsoft\Windows Terminal\Fragments\SerenaTerminal\sereno.json
Total: 3 mkdir, 6 create.
Na primeira vez que vê o perfil Serena, o Windows Terminal adiciona uma entrada curta para ele ao settings.json dele mesmo, como faz com todo perfil de fragmento. O sereno não grava esse arquivo. Use --no-profile para instalar só os esquemas de cores.
Isto foi só uma simulação. Para valer, rode o mesmo comando sem --dry-run.
Nada foi alterado.
```

No Linux, com uma pasta pessoal vazia (mostrada como `/home/user`):

```text
sereno dev: plan
  mkdir   /home/user/.local
  mkdir   /home/user/.local/share
  mkdir   /home/user/.local/share/sereno
  mkdir   /home/user/.local/share/sereno/shell
  create  /home/user/.local/share/sereno/manifest.json
  create  /home/user/.local/share/sereno/shell/init.ps1
  create  /home/user/.local/share/sereno/shell/init.bash
  create  /home/user/.local/share/sereno/shell/init.zsh
  create  /home/user/.local/share/sereno/mode
Total: 4 mkdir, 5 create.
Isto foi só uma simulação. Para valer, rode o mesmo comando sem --dry-run.
Nada foi alterado.
```

O que são esses arquivos:

| Arquivo | O que é |
| :--- | :--- |
| `sereno\shell\init.ps1`, `init.bash`, `init.zsh` | A preparação da sessão para PowerShell, bash e zsh: prompt, cores e a verificação que acompanha o sistema. São os mesmos bytes para todo usuário; não imprimem nada e não iniciam nenhum programa. |
| `sereno\mode` | Cinco linhas com as configurações: `mode`, `flavor`, `inband`, `keys`, `prompt`. |
| `sereno\manifest.json` | O registro de cada arquivo e pasta que o sereno gravou, com o SHA-256 de cada arquivo. É por ele que o `uninstall` se guia. |
| `Fragments\SerenaTerminal\sereno.json` | Um fragmento para o Windows Terminal: o perfil "Serena" e os seis esquemas de cores. Só é gravado no Windows, e só quando o Windows Terminal é encontrado. |

A pasta do sereno é `%LOCALAPPDATA%\sereno` no Windows e `~/.local/share/sereno` no macOS e no Linux (`$XDG_DATA_HOME/sereno` quando essa variável está definida). A variável `SERENO_HOME` muda a pasta de lugar. Outros comandos acrescentam coisas a ela: o `sereno export` grava `themes`, uma linha de hook num arquivo de inicialização que já existia cria `backups`, e enquanto um comando está gravando existe ali um arquivo vazio chamado `lock`.

Por padrão, nenhum arquivo seu é editado. Só quando você pede com `--hook` o sereno acrescenta uma linha ao arquivo de inicialização de um shell. Um arquivo que já existe é antes copiado para `backups`, e o plano mostra `append`, como aqui; quando ainda não existe nenhum, o sereno cria o arquivo com essa única linha (o plano mostra `create`), e não há o que copiar:

```text
  append  C:\Users\user\Documents\WindowsPowerShell\Microsoft.PowerShell_profile.ps1
          + if ($ExecutionContext.SessionState.LanguageMode -eq 'FullLanguage' -and [System.IO.File]::Exists("$env:LOCALAPPDATA\sereno\shell\init.ps1")) { . "$env:LOCALAPPDATA\sereno\shell\init.ps1" } # sereno:hook
  append  C:\Users\user\.bashrc
          + [ -r "$LOCALAPPDATA/sereno/shell/init.bash" ] && . "$LOCALAPPDATA/sereno/shell/init.bash" # sereno:hook
```

O sereno nunca grava o `settings.json` nem o `state.json` do Windows Terminal, o registro, o `PATH`, uma política de execução, um shell de login ou a pasta de configuração de qualquer emulador. Ele não instala nenhuma ferramenta, fonte, shell ou emulador. Quem grava é o próprio Windows Terminal: na primeira vez que vê o perfil Serena, ele acrescenta uma entrada curta para o perfil ao `settings.json` dele, como o plano avisa. O [docs/uninstall.md](docs/uninstall.md) explica o que isso significa na hora de remover o sereno.

## Instalação

Colocar o programa na máquina não altera nada: o sereno só grava quando você roda `sereno apply`.

### A partir do código-fonte

Isto funciona hoje. É preciso ter o Go 1.24 ou mais novo. Numa cópia deste repositório:

```sh
go build ./cmd/sereno
```

O resultado é o `sereno` (`sereno.exe` no Windows) na pasta atual. Rode-o pelo caminho, ou mova-o para uma pasta que esteja no seu `PATH`.

### Com `go install`

Disponível quando a tag `v0.1.0` existir:

```sh
go install github.com/joaooliveira10/serena-terminal/cmd/sereno@v0.1.0
```

Um programa instalado assim informa a versão como `dev`: a tag só é gravada nos programas dos arquivos de release.

### A partir de um arquivo de release

Disponível quando a primeira release existir. Uma release tem um arquivo compactado por sistema, com o nome `sereno_<version>_<os>_<arch>.zip` para Windows e `.tar.gz` para macOS (`darwin`) e Linux, para `amd64` e `arm64`. Cada um contém o programa, o `LICENSE` e o `README.md`. O `checksums.txt` traz o SHA-256 dos seis arquivos: compare com o que o `Get-FileHash` (Windows), o `sha256sum` (Linux) ou o `shasum -a 256` (macOS) mostram para o arquivo que você baixou. A release também traz um atestado de onde os arquivos foram compilados, que a linha de comando do GitHub confere:

```sh
gh attestation verify sereno_v0.1.0_linux_amd64.tar.gz --repo joaooliveira10/serena-terminal
```

Os programas não são assinados. Não foi testado como o Windows SmartScreen, os antivírus ou o Gatekeeper do macOS lidam com eles.

### Com o script de instalação de uma release

Disponível quando a primeira release existir. Cada release tem o seu `install.ps1` (Windows) e o seu `install.sh` (macOS, Linux). O script baixa o arquivo da própria release para o seu processador, compara o SHA-256 com o valor que foi escrito no script quando a release foi compilada, para se os dois forem diferentes e coloca apenas o programa na pasta `bin` da pasta do sereno. Ele não mexe no `PATH` nem em nenhuma outra configuração e não inicia o programa. Baixe o script, leia e só então rode:

```powershell
Invoke-WebRequest -UseBasicParsing -Uri https://github.com/joaooliveira10/serena-terminal/releases/download/v0.1.0/install.ps1 -OutFile install.ps1
powershell -NoProfile -ExecutionPolicy Bypass -File install.ps1
```

```sh
curl --fail --location --output install.sh https://github.com/joaooliveira10/serena-terminal/releases/download/v0.1.0/install.sh
sh install.sh
```

O script mostra o caminho completo do programa e o próximo comando. Como o `PATH` não é alterado, você chama o programa por esse caminho até aplicá-lo: depois do `sereno apply`, uma sessão que carrega os arquivos do sereno passa a ter um comando `sereno` próprio. `install.ps1 -Uninstall` e `sh install.sh --uninstall` removem o programa (rode `sereno uninstall` antes). Numa máquina sem rede, baixe o arquivo em outro lugar, coloque-o numa pasta e indique essa pasta na variável `SERENO_INSTALL_FROM`: o script pega o arquivo dali e compara o checksum do mesmo jeito. A etapa de download dos scripts ainda não rodou contra uma release de verdade, porque não existe nenhuma; todo o resto deles é testado contra uma pasta local.

## Comandos

Comece por `sereno apply --dry-run`. Todos os exemplos abaixo são a saída de uma execução real numa pasta pessoal de teste. O `sereno help` lista os comandos, e `sereno help COMMAND` explica um deles.

Em todos os comandos: `--lang en|pt-BR` escolhe o idioma das mensagens (sem ela, vale a variável `SERENO_LANG` e depois o idioma do sistema), `--no-color` e a variável `NO_COLOR` desligam as cores da saída do próprio sereno, e `-h` ou `--help` mostra a ajuda.

| Código de saída | Significado |
| :--- | :--- |
| 0 | Feito, mesmo quando não havia nada a fazer. |
| 1 | Algo falhou durante a execução; no `check`, uma cor está abaixo do piso. Também: outro sereno estava gravando a mesma pasta, e este cedeu a vez. |
| 2 | A linha de comando não foi entendida. |
| 3 | Recusado antes de gravar qualquer coisa, com o motivo e a saída. |
| 4 | Terminou, mas algo ficou como estava: um arquivo que você editou depois que o sereno o gravou. |

### apply

Grava os arquivos do plano e termina dizendo o que vem em seguida. No Windows com Windows Terminal: "Feche todas as janelas do Windows Terminal, abra o Windows Terminal de novo e escolha Serena no menu de nova aba. O esperado é que mais uma janela de um Windows Terminal que já está aberto não mostre o que este apply mudou." O que isso diz do Windows Terminal foi lido no código-fonte da versão 1.24.12741.0; o passo ainda não foi experimentado numa máquina de verdade (veja Situação). No macOS e no Linux: "Nada carrega esses arquivos ainda. Para usá-los, deixe o sereno adicionar uma linha ao arquivo de inicialização do seu shell: `sereno apply --hook bash` (ou zsh, pwsh)."

O comando pode ser rodado de novo a qualquer momento; a segunda execução encontra tudo como desejado:

```sh
sereno apply
```

```text
sereno dev: plan
  keep    C:\Users\user\AppData\Local\sereno\manifest.json (já está como desejado)
  keep    C:\Users\user\AppData\Local\sereno\shell\init.ps1 (já está como desejado)
  keep    C:\Users\user\AppData\Local\sereno\shell\init.bash (já está como desejado)
  keep    C:\Users\user\AppData\Local\sereno\shell\init.zsh (já está como desejado)
  keep    C:\Users\user\AppData\Local\sereno\mode (já está como desejado)
  keep    C:\Users\user\AppData\Local\Microsoft\Windows Terminal\Fragments\SerenaTerminal\sereno.json (já está como desejado)
Total: 6 keep.
Nada a fazer: nenhum arquivo foi gravado.
```

| Opção | O que faz |
| :--- | :--- |
| `--dry-run` | Mostra o plano e não altera nada. |
| `--hook LIST` | Acrescenta também uma linha ao arquivo de inicialização destes shells, separados por vírgula: `powershell`, `pwsh`, `bash`, `zsh`. É isso que leva o sereno ao terminal do VS Code e a perfis que não sejam o "Serena". |
| `--shell auto\|powershell\|pwsh` | O PowerShell que o perfil Serena abre. `auto` usa o PowerShell 7 quando ele está instalado e, se não, o Windows PowerShell 5.1. |
| `--no-profile` | Só os seis esquemas de cores, sem o perfil Serena. |
| `--keys profile\|always\|off` | Onde valem as teclas de conforto do PSReadLine: no perfil Serena, em toda sessão ou nunca. |
| `--prompt on\|off` | O prompt Serena: caminho, branch, resultado do último comando. |

As opções que você não informa continuam com o valor do apply anterior. Isso vale para `--no-profile` (para ter o perfil de volta: `sereno uninstall` e depois `sereno apply`) e para as linhas de hook: um hook pedido uma vez é conferido em todo apply seguinte, no arquivo em que foi colocado (o PowerShell só é consultado de novo quando você o indica outra vez em `--hook`, ou quando esse arquivo não existe mais), e `sereno uninstall --hook LIST` o retira.

### uninstall

Desfaz o apply. Com `--dry-run`, mostra o que faria:

```sh
sereno uninstall --dry-run
```

```text
sereno dev: plan
  remove  C:\Users\user\AppData\Local\Microsoft\Windows Terminal\Fragments\SerenaTerminal\sereno.json
  remove  C:\Users\user\AppData\Local\sereno\shell\init.bash
  remove  C:\Users\user\AppData\Local\sereno\shell\init.ps1
  remove  C:\Users\user\AppData\Local\sereno\shell\init.zsh
  remove  C:\Users\user\AppData\Local\sereno\mode
  remove  C:\Users\user\AppData\Local\sereno\manifest.json
  rmdir   C:\Users\user\AppData\Local\Microsoft\Windows Terminal\Fragments\SerenaTerminal
  rmdir   C:\Users\user\AppData\Local\sereno\shell
  rmdir   C:\Users\user\AppData\Local\sereno
Total: 6 remove, 3 rmdir.
O programa sereno em si continua onde está: C:\Users\user\go\bin\sereno.exe. Apague esse arquivo para removê-lo.
Isto foi só uma simulação. Para valer, rode o mesmo comando sem --dry-run.
Nada foi alterado.
```

`--hook LIST` retira apenas a linha de hook dos arquivos dos shells indicados e deixa todo o resto.

### theme

Sem argumentos, mostra as configurações, o que o sistema informa e a variante que resulta disso. Com um modo (`auto`, `dark`, `light`, `hc`, `hc-light`), `--flavor soft|vivid` ou `--inband profile|always|off`, muda a configuração para todas as sessões:

```sh
sereno theme light --flavor vivid
```

```text
  replace C:\Users\user\AppData\Local\sereno\mode
  replace C:\Users\user\AppData\Local\Microsoft\Windows Terminal\Fragments\SerenaTerminal\sereno.json
As sessões que acompanham o sistema leem a nova configuração no próximo prompt; as outras, quando forem abertas.
mode                light
flavor              vivid
inband              profile
keys                profile
prompt              on
tema do sistema     escuro, sem tema de contraste
variante            light-vivid (Serena Light Vivid)
rota da paleta      emulator: vale o esquema de cores do próprio emulador
```

`--show` mostra também os dezesseis slots de cor, uma amostra de cada papel e o contraste medido. `--paint VARIANT` pinta o terminal em que você está com uma destas: `dark`, `dark-vivid`, `light`, `light-vivid`, `hc`, `hc-light`; `--reset` devolve ao terminal as cores dele. Os dois não gravam nada em disco e funcionam sem apply, então `sereno theme --paint light` é um jeito de experimentar uma paleta sem instalar nada. A pintura é recusada onde não pode funcionar: quando a saída não é um terminal, no terminal do VS Code e dentro do tmux ou do screen.

### status

Lê e informa; não grava nada e não inicia nada. Os avisos dizem o que pode surpreender e o que fazer a respeito:

```sh
sereno status
```

```text
versão              sereno dev (windows/amd64)
pasta               C:\Users\user\AppData\Local\sereno
aplicado            sim
idioma              pt-BR
mode                auto
flavor              soft
inband              profile
keys                profile
prompt              on
tema do sistema     escuro, sem tema de contraste
variante            dark (Serena Dark)
rota da paleta      emulator: vale o esquema de cores do próprio emulador
sessão              outro terminal (nem Windows Terminal nem VS Code); aberta pelo perfil Serena: não
Windows Terminal    detectado; fragmento do sereno instalado, com o perfil Serena
                    configurações: C:\Users\user\AppData\Local\Packages\Microsoft.WindowsTerminal_8wekyb3d8bbwe\LocalState\settings.json
                    tema: não definido (nesse caso o Windows Terminal usa dark)
                    entrada própria para o perfil Serena: não; já viu o perfil: não
linhas de hook      nenhuma
arquivos do sereno  5, sendo 0 editados e 0 ausentes
PowerShell 7        não está no PATH

avisos
  wt.theme-not-system
      O tema do próprio Windows Terminal não acompanha o Windows (está em dark, o padrão dele), então o par de esquemas claro/escuro do perfil Serena também não acompanha. A rota in-band do sereno acompanha, a cada prompt. Para o par acompanhar também, vá em Aparência, nas configurações do Windows Terminal, e escolha a opção que usa o tema do Windows.
  pwsh.absent
      O PowerShell 7 (pwsh) não está no PATH. Não falta nada: o Windows PowerShell 5.1 tem suporte completo. Se você quiser o PowerShell 7, este comando o instala, e o sereno nunca o roda por você: winget install -e --id Microsoft.PowerShell --source winget
```

`--json` mostra o mesmo relatório em JSON, com chaves que não são traduzidas.

### check

Mede cada cor das seis variantes contra o fundo (razão de contraste da WCAG) e termina com código de saída 1 quando alguma está abaixo do piso: 4.5 para dark e light, 7.0 para alto contraste.

```sh
sereno check
```

```text
dark         mínimo 4.52:1 no slot 8, piso 4.5: passa
dark-vivid   mínimo 4.52:1 no slot 8, piso 4.5: passa
light        mínimo 4.51:1 no slot 11, piso 4.5: passa
light-vivid  mínimo 4.51:1 no slot 11, piso 4.5: passa
hc           mínimo 7.82:1 no slot 1, piso 7.0: passa
hc-light     mínimo 7.28:1 no slot 15, piso 7.0: passa
Todos os valores estão no piso ou acima dele.
```

`--json` entrega as medições em JSON, e `--markdown`, a tabela que fica no repositório como [docs/contrast.md](docs/contrast.md).

### export

Grava seis arquivos de tema por emulador e mostra para onde copiá-los e a linha que os ativa. Nada é gravado nas pastas do próprio emulador.

```sh
sereno export --target kitty
```

```text
sereno dev: plan
  mkdir   C:\Users\user\AppData\Local\sereno\themes
  mkdir   C:\Users\user\AppData\Local\sereno\themes\kitty
  replace C:\Users\user\AppData\Local\sereno\manifest.json
  create  C:\Users\user\AppData\Local\sereno\themes\kitty\Serena Dark.conf
  create  C:\Users\user\AppData\Local\sereno\themes\kitty\Serena Dark Vivid.conf
  create  C:\Users\user\AppData\Local\sereno\themes\kitty\Serena Light.conf
  create  C:\Users\user\AppData\Local\sereno\themes\kitty\Serena Light Vivid.conf
  create  C:\Users\user\AppData\Local\sereno\themes\kitty\Serena High Contrast.conf
  create  C:\Users\user\AppData\Local\sereno\themes\kitty\Serena High Contrast Light.conf
Total: 2 mkdir, 6 create, 1 replace.
Pronto.

kitty: copie C:\Users\user\AppData\Local\sereno\themes\kitty\Serena Dark.conf como dark-theme.auto.conf e C:\Users\user\AppData\Local\sereno\themes\kitty\Serena Light.conf como light-theme.auto.conf para a pasta de configuração do kitty; a partir daí ele alterna sozinho.
Esses quatro formatos são gerados a partir da paleta e não foram conferidos na tela dos emuladores.
Os arquivos fazem parte da pasta do sereno: o `sereno uninstall` os remove.
```

`--target all|ghostty|kitty|wezterm|alacritty` escolhe o emulador (o padrão são os quatro). `--to DIR` grava numa pasta que você indica, em vez da pasta do sereno; nesse caso o sereno não guarda registro dos arquivos, e o comando funciona sem apply.

### version

```sh
sereno version
```

```text
sereno dev (windows/amd64)
```

Um programa que vem de um arquivo de release mostra a tag da release no lugar de `dev`.

### help

```sh
sereno help theme
```

```text
Uso: sereno theme [auto|dark|light|hc|hc-light] [--flavor soft|vivid] [--inband profile|always|off] [--show] [--paint VARIANT] [--reset]

Sem argumentos, mostra as configurações, o que o sistema informa e a variante que resulta disso. Um modo, --flavor ou --inband muda a configuração para todas as sessões. --paint e --reset só escrevem neste terminal e não precisam de apply.

  auto|dark|light|hc|hc-light   auto acompanha o sistema; os outros fixam uma aparência (hc é alto contraste)
  --flavor soft|vivid           cores mais suaves ou mais vivas para dark e light
  --inband profile|always|off   onde o sereno repinta o terminal no prompt: no perfil Serena, em toda sessão que o próprio Windows Terminal iniciou ou nunca
  --show                        mostra também os 16 slots, uma amostra de cada papel e o contraste medido
  --paint VARIANT               pinta este terminal agora: dark, dark-vivid, light, light-vivid, hc ou hc-light
  --reset                       devolve a este terminal as cores dele
```

## Como funciona o acompanhamento do sistema, e seus limites

No Windows, o sereno lê três valores do registro do usuário atual, e só lê: se os aplicativos usam o tema claro, se um tema de contraste está ligado e a cor de janela desse tema de contraste (que distingue um tema de contraste claro de um escuro). Com `mode=auto`, eles escolhem uma das seis variantes. Uma sessão aberta pelo perfil Serena confere esses valores antes de cada prompt, dentro do próprio shell e sem iniciar nenhum programa, e, quando a variante não é a que ela pintou por último, envia ao Windows Terminal as dezesseis cores, a cor do texto, a do fundo, a do cursor e a da seleção. Assim, depois que você troca o Windows entre claro e escuro, a aba aberta muda no próximo prompt: aperte Enter. Não existe processo em segundo plano, e nada muda enquanto o shell está parado.

Os limites, sem rodeios. O que o sereno faz vem do código e dos testes dele. Do que o Windows Terminal faz com isso na tela, uma coisa foi vista: uma aba aberta repintada pelo `sereno theme --paint light` (veja Situação). O resto ainda não foi conferido e aparece como esperado onde isso importa:

- **O primeiro quadro.** Uma aba Serena nova abre com o esquema de cores que o Windows Terminal escolhe e é repintada no primeiro prompt. A configuração de tema do próprio Windows Terminal é "dark" enquanto você não a muda, então, com o Windows em modo claro, o esperado é que uma aba nova comece escura por um instante. Na única máquina em que isso foi medido (Windows PowerShell 5.1), o sereno leva cerca de 70 ms para carregar e mais uns 60 ms no primeiro prompt, além do tempo de abertura do próprio PowerShell.
- **A configuração de tema do próprio Windows Terminal.** O perfil Serena também indica um esquema escuro e um claro, como um par. O Windows Terminal só alterna esse par quando a configuração dele, em Aparência, é a que usa o tema do Windows; nesse caso, o esperado é que ele também escolha o esquema certo para o primeiro quadro. O sereno não muda essa configuração por você; o `sereno status` avisa quando ela não está assim (`wt.theme-not-system`).
- **Só uma mudança é pintada.** Uma sessão envia as cores quando a variante não é a que ela enviou por último. Se o próprio Windows Terminal devolver uma aba aberta ao esquema do perfil (o esperado é que ele faça isso ao recarregar as configurações), a aba fica assim até a configuração do sistema mudar de novo, ou até você rodar `sereno theme` nessa aba, o que envia as cores mais uma vez.
- **Outros perfis do Windows Terminal.** Só as sessões abertas pelo perfil Serena são repintadas. Uma sessão de outro perfil que carrega os arquivos do sereno por uma linha de hook mostra o prompt e as cores do editor de linha do sereno no esquema de cores do próprio perfil, que o sereno não mediu (o [docs/support-matrix.md](docs/support-matrix.md) traz os números calculados para o esquema que vem com o Windows Terminal). `sereno theme --inband always` estende a repintura a toda sessão que o próprio Windows Terminal iniciou e que carrega os arquivos do sereno por uma linha de hook, e `--inband off` a desliga.
- **O Windows Terminal como terminal padrão.** O sereno reconhece uma sessão do Windows Terminal pela variável `WT_SESSION`, que o Windows Terminal define para as sessões que ele mesmo inicia. O esperado é que um PowerShell que o Windows abre sozinho dentro do Windows Terminal (pelo menu Iniciar, pela caixa Executar ou por um atalho, quando o Windows Terminal é o aplicativo de terminal padrão) não a tenha. O sereno então trata a sessão como se estivesse fora do Windows Terminal: ela não é repintada, nem com `--inband always`, o prompt não leva as marcas, e o último item desta lista decide sobre as cores dela. Isso é o esperado pela regra e por um relato ainda aberto contra o Windows Terminal; nenhuma aba assim foi aberta.
- **O terminal do VS Code.** O sereno nunca repinta ali: a paleta é do tema de cores do editor. Com uma linha de hook você tem o prompt e as cores do editor de linha, e as cores em si vêm do tema que você escolheu no VS Code.
- **macOS e Linux.** A versão 0.1 não detecta a aparência do sistema neles. `mode=auto` conta como dark, e nada é repintado. O caminho com suporte é o par claro/escuro do seu emulador, com os arquivos de tema do `sereno export`.
- **tmux e screen** ficam entre o sereno e o terminal: ali, `--paint` e `--reset` são recusados.
- **Windows sem Windows Terminal** (a janela de console clássica, ou outro emulador) recebe o prompt e as cores do editor de linha por uma linha de hook, e nenhuma paleta: as cores são então as desse console ou emulador. O sereno não sabe que janela é essa. Ele se guia pelo que a sessão traz: não há `WT_SESSION`, não é o terminal do VS Code, e o console indica um fundo que não é preto. Onde as três coisas valem, o sereno não escreve nenhuma das suas cores: o prompt é texto puro, o editor de linha fica com as cores que tem, e os estilos do PowerShell 7 também. A janela que o Windows PowerShell abre sozinho é um console assim: o fundo dela é a cor que o sereno usaria para palavras-chave e para o nome do branch. Nada disso foi conferido numa tela.

O [docs/how-it-works.md](docs/how-it-works.md) explica os três caminhos pelos quais a paleta chega à tela, o arquivo de configurações e quanto custa a verificação antes de cada prompt.

## Suporte

Nível 1 quer dizer: o mantenedor tem isso na máquina dele, e é ali que é testado. Nível 2 quer dizer: gerado e testado por programas, sem que o mantenedor tenha olhado numa tela.

| | Situação hoje |
| :--- | :--- |
| Windows 11, Windows Terminal, Windows PowerShell 5.1 | Nível 1. Os testes automáticos passam na máquina do mantenedor. Na tela, uma coisa foi vista ali (uma aba aberta repintada, veja Situação); o teste de aceitação com um perfil de verdade ainda está por fazer. |
| Windows 11, PowerShell 7 | Nível 1 por projeto, com o mesmo arquivo de sessão. Os testes automáticos passam no CI com o PowerShell 7.6.6; ele não está instalado na máquina do mantenedor, então ali nada rodou nem foi visto. |
| Git Bash no Windows | Nível 2. Testado. |
| bash no Linux | Nível 2. Testado no Ubuntu, no WSL, e no CI. |
| bash 3.2 e zsh no macOS, zsh no Linux | Nível 2. Testado só no CI (bash 3.2.57 e zsh 5.9 no macOS, zsh 5.9 no Ubuntu). |
| Arquivos de tema para Ghostty, kitty, WezTerm, Alacritty | Nível 2. Gerados e conferidos contra a gramática de cada formato; não conferidos nos emuladores. |

O [docs/support-matrix.md](docs/support-matrix.md) traz os detalhes, incluindo os limites conhecidos de cada shell.

## Desinstalação, e o que fica

O `sereno uninstall` retira as linhas de hook que acrescentou, dos arquivos em que as colocou, apaga cada arquivo que gravou e que continua como ele gravou, e remove as pastas que criou. Um arquivo que você editou depois fica onde está e é informado (código de saída 4). O que ele não remove, e avisa quando é o caso:

- a entrada que o próprio Windows Terminal acrescentou ao `settings.json` dele para o perfil Serena, e o identificador do perfil no `state.json` dele. O sereno não grava esses arquivos. Deixe a entrada: depois que o Windows Terminal for fechado e aberto de novo, o esperado é que ele tire o Serena do menu de nova aba e continue a listá-lo nas configurações, como um perfil que não é mais detectado (lido no código-fonte da versão 1.24, ainda não visto);
- o programa sereno;
- a pasta `bin` dentro da pasta do sereno, quando foi o script de instalação que colocou o programa ali;
- o backup de um arquivo de inicialização que você alterou depois que a linha de hook foi acrescentada.

Ele procura uma linha de hook no arquivo em que ela foi registrada, e em nenhum outro lugar. Um arquivo de inicialização que mudou de lugar depois disso fica com a linha do sereno: o uninstall avisa que não há arquivo no lugar registrado, e não procura o que foi movido. Um uninstall deixa a linha sem avisar. O `sereno apply` recusa um perfil que mudou de lugar e indica `sereno uninstall --hook powershell`; quando o hook é pedido de novo depois disso (`sereno apply --hook powershell`), o sereno encontra a linha dele no arquivo movido e a trata como sua, e um uninstall posterior a deixa onde está e não diz nada. A mudança da qual se espera isso é a de uma pasta Documentos para o OneDrive, com o perfil do PowerShell dentro.

O [docs/uninstall.md](docs/uninstall.md) lista todos os casos.

## Privacidade

O sereno não tem código de rede: não abre nenhuma conexão, não envia telemetria, não tem conta e não procura atualizações. Um teste falha se o programa passar a incluir os pacotes de rede do Go. Os arquivos de sessão que ele gera também não iniciam nenhum programa nem abrem conexão. O [docs/privacy.md](docs/privacy.md) diz o que ele lê na sua máquina.

## Relação com o Serena Theme

O [Serena Theme](https://github.com/joaooliveira10/serena-theme) é um tema de cores para o VS Code, do mesmo autor. O Serena Terminal tira a paleta dele: o `internal/palette/serena.json` é gerado a partir dos seis arquivos de tema do Serena Theme v1.1.0 e é o único arquivo-fonte daqui que contém valores de cor. São dois programas separados; nenhum precisa do outro. No terminal do próprio VS Code a paleta pertence ao tema, e é por isso que o sereno não repinta ali.

## Licença

MIT, a mesma do Serena Theme. Veja o [LICENSE](LICENSE). Relatos de segurança: [SECURITY.md](SECURITY.md). Mudanças: [CHANGELOG.md](CHANGELOG.md). O SECURITY.md, o CHANGELOG.md e os documentos da pasta `docs` estão em inglês.
