# readme hero: the tui on the mock network, wallet popup, pick a scenario, watch it pass
# run from the repo root: vhs demo/hero.tape
Output demo/assets/hh.gif

Require vhs
Set Shell "bash"
Set FontSize 16
Set Width 1400
Set Height 820
Set Padding 20
Set Framerate 24
Set TypingSpeed 40ms
Set WaitTimeout 30s
Set Theme { "name": "pantera", "background": "#201F26", "foreground": "#ECEBF0", "black": "#2D2C36", "red": "#FF577D", "green": "#12C78F", "yellow": "#F5EF34", "blue": "#6B50FF", "magenta": "#FF60FF", "cyan": "#00A4FF", "white": "#BFBCC8", "brightBlack": "#4D4C57", "brightRed": "#FF6DAA", "brightGreen": "#00FFB2", "brightYellow": "#E8FE96", "brightBlue": "#7272FF", "brightMagenta": "#FF84FF", "brightCyan": "#4FBEFE", "brightWhite": "#F7F6FB", "selection": "#3A3943", "cursor": "#FF60FF" }
Env PS1 "$ "
Env HH_NETWORK "mock"

Hide
Type "go build -o hh . && export PATH=$PWD:$PATH && clear" Enter
Sleep 4s
Show

Type "hh" Enter
Sleep 2500ms
Ctrl+W
Sleep 2500ms
Escape
Sleep 500ms
Ctrl+R
Sleep 1s
Type "airdrop"
Sleep 800ms
Enter
Wait+Screen /9\/9/
Sleep 4s
Ctrl+C
Sleep 300ms
Ctrl+C
