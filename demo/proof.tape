# the judge layer: same scenario on the in process mock and on real testnet
# needs .env with a funded portal account. run from the repo root: vhs demo/proof.tape
Output demo/out/proof.mp4

Require vhs
Set Shell "bash"
Set FontSize 18
Set Width 1600
Set Height 1000
Set Padding 24
Set WindowBar Colorful
Set TypingSpeed 35ms
Set WaitTimeout 120s
Set Theme { "name": "pantera", "background": "#201F26", "foreground": "#ECEBF0", "black": "#2D2C36", "red": "#FF577D", "green": "#12C78F", "yellow": "#F5EF34", "blue": "#6B50FF", "magenta": "#FF60FF", "cyan": "#00A4FF", "white": "#BFBCC8", "brightBlack": "#4D4C57", "brightRed": "#FF6DAA", "brightGreen": "#00FFB2", "brightYellow": "#E8FE96", "brightBlue": "#7272FF", "brightMagenta": "#FF84FF", "brightCyan": "#4FBEFE", "brightWhite": "#F7F6FB", "selection": "#3A3943", "cursor": "#FF60FF" }

Env PS1 "$ "

Hide
Type "go build -o hh . && export PATH=$PWD:$PATH && clear" Enter
Sleep 4s
Show

Type "hh run examples/05-scheduled-multisig.yaml" Enter
Wait+Screen /passed|failed/
Sleep 3s
Type "clear && hh run examples/05-scheduled-multisig.yaml -n testnet" Enter
Wait+Screen@120s /passed ·|failed ·/
Sleep 4s
