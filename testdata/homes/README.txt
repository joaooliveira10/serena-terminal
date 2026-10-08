Fixture homes for the tests. testenv.NewFrom(t, "<name>") copies one of these
trees into a temporary home.

A top-level directory whose name starts with @ is an alias: it is copied to
the long place it stands for, so that no path in the repository gets too long
for a plain Git checkout on Windows.

  @wt-state      AppData/Local/Packages/Microsoft.WindowsTerminal_8wekyb3d8bbwe/LocalState
  @wt-fragments  AppData/Local/Microsoft/Windows Terminal/Fragments

The table lives in internal/testenv/testenv.go.

Every file here is compared byte for byte (.gitattributes: testdata/** -text).
Line endings, byte order marks and encodings are part of the fixtures.
