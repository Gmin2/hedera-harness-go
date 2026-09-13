# readme: a scenario on the mock network in milliseconds, then a failure with evidence
# run from the repo root: vhs demo/run.tape
Output demo/assets/run.gif

Require vhs
Set Shell "bash"
Set FontSize 16
Set Width 1400
Set Height 860
Set Padding 20
Set Framerate 24
Set TypingSpeed 40ms
Set Theme { "name": "pantera", "background": "#201F26", "foreground": "#ECEBF0", "black": "#2D2C36", "red": "#FF577D", "green": "#12C78F", "yellow": "#F5EF34", "blue": "#6B50FF", "magenta": "#FF60FF", "cyan": "#00A4FF", "white": "#BFBCC8", "brightBlack": "#4D4C57", "brightRed": "#FF6DAA", "brightGreen": "#00FFB2", "brightYellow": "#E8FE96", "brightBlue": "#7272FF", "brightMagenta": "#FF84FF", "brightCyan": "#4FBEFE", "brightWhite": "#F7F6FB", "selection": "#3A3943", "cursor": "#FF60FF" }
Env PS1 "$ "

Hide
Type "go build -o hh . && export PATH=$PWD:$PATH && clear" Enter
Sleep 4s
Show

Type "cat examples/01-first-transfer.yaml" Enter
Sleep 2500ms
Type "clear && hh run examples/01-first-transfer.yaml" Enter
Sleep 3s
Type "clear && sed 's/equals: 12.5/equals: 13/' examples/01-first-transfer.yaml > /tmp/hh-broken.yaml && hh run /tmp/hh-broken.yaml" Enter
Sleep 4s
