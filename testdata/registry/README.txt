Stand-ins for /proc/registry/HKEY_CURRENT_USER, the place where Git Bash
shows the registry as files. init.bash and init.zsh read the three signals of
requirement R28 from there; the tests point SERENO_REG_ROOT at one of these
trees instead, so the same test runs on Windows, macOS and Linux and never
depends on how the machine is set up.

A tree is named by one digit, the three signals as bits:

    digit = 4 * hc_on + 2 * hc_light + apps_light

    Control Panel/Accessibility/HighContrast/Flags   text, "127" (on) or "126" (off)
    Control Panel/Colors/Window                      text, "255 255 255" or "0 0 0"
    Software/Microsoft/Windows/CurrentVersion/Themes/Personalize/AppsUseLightTheme
                                                     four bytes, 01 00 00 00 or 00 00 00 00

The text values end with a NUL byte and have no newline, as Git Bash shows
them. The names are single digits because of the limit of 100 characters on
every path of the repository (internal/policy, TestPathsStayShort).
