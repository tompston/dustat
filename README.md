## dustat

_find exported but unused values in a Go project_

A small tool used for a simple cleanups of Go projects. Finds exported values (functions, structs, variables, etc.) that are never used.

No external dependencies.

### Installation

```bash
# install the binary
go install github.com/tompston/dustat@latest
# or clone the repo and run go run main.go
git clone https://github.com/tompston/dustat.git
```

### Usage

_note that the path to the `go/bin` directory must be in your PATH environment variable_

```bash
# point to the directory of the Go project
dustat <path-to-dir>
# point to the directory, but do not include certain names
dustat --ignore=MyFuncName,MyStructName <path-to-dir>
```

### Examples

```bash
# clone a big example project locally to a tmp dir
git clone git@github.com:ethereum/go-ethereum.git tmp/go-eth
go run main.go ./tmp/go-eth

## Snippet of the output
...
37    Stacks (tmp/go-eth/internal/debug/api.go:192:18)
37    NewPendingTransactions (tmp/go-eth/eth/filters/api.go:164:23)
39    DumpBlock (tmp/go-eth/eth/api_debug.go:50:22)
49    Resend (tmp/go-eth/internal/ethapi/api.go:1645:28)
57    GetBlobsV2 (tmp/go-eth/eth/catalyst/api.go:526:26)
58    GetAccessibleState (tmp/go-eth/eth/api_debug.go:364:22)
61    IntermediateRoots (tmp/go-eth/eth/tracers/api.go:508:17)
62    AccountRange (tmp/go-eth/eth/api_debug.go:136:22)
70    Syslog (tmp/go-eth/metrics/syslog.go:14:6)
========================================================
Total Unused Lines: 3175, Declarations: 571
```


<!-- 

## publising

git add .
git commit -m "dustat: release v0.0.1"
git tag v0.0.1
git push origin v0.0.1
GOPROXY=proxy.golang.org go list -m github.com/tompston/dustat@v0.0.1

qwe
 -->