Reviewed renderings of everything sereno generates (test plan T4). The test
internal/tools/golden compares them, byte for byte, with what the renderer
produces; a normal test run writes nothing here.

    fragment/   the Windows Terminal fragment: auto with both flavours, every
                fixed mode, without the profile, for PowerShell 7, and with a
                ROOT that is not the default one
    init/       init.ps1, init.bash and init.zsh as `sereno apply` writes them
                to ROOT/shell
    palette/    PALETTE of the six variants and RESET, the bytes written to
                the terminal (requirement R29)
    export/     the 24 theme files of `sereno export`

After a deliberate change to a template, to the palette or to the role table,
regenerate them from the root of the repository and read the diff before
committing it:

    go run ./internal/tools/golden --out testdata/golden

A file the renderer no longer produces has to be deleted by hand; the test
names it.

Every file here is compared byte for byte (.gitattributes: testdata/** -text).
