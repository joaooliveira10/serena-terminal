package i18n

// portuguese is the Brazilian Portuguese catalogue: a faithful translation of
// english (en.go), key for key and argument for argument.
var portuguese = map[string]string{
	// Saída do plano (R13, R14). Os verbos não são traduzidos.
	"plan.nothing-changed":    "Nada foi alterado.",
	"plan.note.kept":          "já está como desejado",
	"plan.note.edited":        "editado depois que o sereno o gravou; mantido como está",
	"plan.note.not-regular":   "não é mais um arquivo comum; mantido como está",
	"plan.note.backup-of":     "cópia de %s de antes da linha de hook",
	"plan.note.backup-kept":   "%s mudou depois que a linha de hook foi adicionada, então o backup fica",
	"plan.note.hook-present":  "a linha de hook já está lá",
	"plan.note.hook-replaces": "substitui a linha do sereno que está lá",
	"plan.note.hook-removed":  "linha de hook removida",
	"plan.note.hook-restored": "de volta ao conteúdo de antes da linha de hook",
	"plan.note.bash-profile":  "o Git for Windows vai criar o ~/.bash_profile sozinho",

	// Recusas (R4 código 3, R15, R20, R22). Cada uma vem depois do caminho.
	"refusal.foreign":            "existe e não foi gravado pelo sereno",
	"refusal.different":          "existe com outro conteúdo",
	"refusal.not-regular":        "não é um arquivo comum",
	"refusal.parent":             "%s está no caminho e não é uma pasta",
	"refusal.unreadable":         "não pode ser lido (%s)",
	"refusal.symlink":            "é um link simbólico",
	"refusal.read-only":          "é somente leitura",
	"refusal.too-large":          "é maior que 1 MiB",
	"refusal.no-room":            "ficaria maior que 1 MiB com a linha de hook",
	"refusal.utf16be":            "está codificado em UTF-16 big endian",
	"refusal.malformed":          "começa como UTF-16, mas não é UTF-16 little endian válido",
	"refusal.binary":             "contém bytes NUL e não é UTF-16 little endian",
	"refusal.hook-moved":         "a linha de hook de %[1]s está registrada em %[2]s; rode `sereno uninstall --hook %[1]s` antes",
	"refusal.backup-blocked":     "está no caminho de um backup e tem outro conteúdo",
	"refusal.shell-absent":       "%s não está no PATH",
	"refusal.shell-windows-only": "o Windows PowerShell 5.1 só existe no Windows; use pwsh aqui",
	"refusal.policy":             "a política de execução do PowerShell é %s, então o perfil não seria carregado",
	"refusal.no-profile-path":    "%[1]s não informou o caminho do perfil (a pasta Documentos pode não existir ou estar inacessível); defina %[2]s com o arquivo de perfil que o sereno deve usar",
	"refusal.probe-timeout":      "%[1]s não respondeu em %[2]s segundos; defina %[3]s com o arquivo de perfil e o sereno não vai perguntar a ele",
	"refusal.root-not-ascii":     "o caminho da pasta do sereno, %s, tem caracteres que uma linha de hook em ASCII não comporta",
	"refusal.advice.add-by-hand": "O sereno não edita este arquivo. Para usar o hook, adicione você mesmo esta linha a ele:",
	"refusal.advice.policy":      "O sereno não muda a política. Para permitir os seus próprios scripts, rode você mesmo:",

	// O que o apply diz sobre o Windows Terminal (R19).
	"wt.notice.stub": "O Windows Terminal adiciona uma entrada curta para o perfil Serena ao seu próprio settings.json na primeira vez que o vê, como faz com todo perfil de fragmento. O sereno não grava esse arquivo. Use --no-profile para instalar só os esquemas de cores.",
	"wt.notice.open": "Abra uma nova janela do Windows Terminal e escolha Serena no menu de nova aba. Se ele não aparecer na lista, feche todas as janelas do Windows Terminal e abra de novo.",
}
