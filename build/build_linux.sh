export GOOS=linux
go build -o build/bin_linux/tcp_client tcp-client/main.go
go build -o build/bin_linux/tun_client tun-client/main.go
go build -o build/bin_linux/tun_server tun-server/main.go
go build -o build/bin_linux/tcp_server tcp-server/main.go
export GOOS=