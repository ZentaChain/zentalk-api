module github.com/ZentaChain/zentalk-api

go 1.24.6

toolchain go1.24.9

require (
	github.com/ZentaChain/zentalk-node v0.0.0
	github.com/ethereum/go-ethereum v1.16.5
	github.com/google/uuid v1.6.0
	github.com/gorilla/mux v1.8.1
	github.com/gorilla/websocket v1.5.3
	github.com/mattn/go-sqlite3 v1.14.32
	golang.org/x/crypto v0.42.0
)

require (
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.4.0 // indirect
	github.com/holiman/uint256 v1.3.2 // indirect
	golang.org/x/sys v0.36.0 // indirect
)

// Use local zentalk-node
replace github.com/ZentaChain/zentalk-node => ../zentalk-node
