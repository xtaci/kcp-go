export GOOS=
go build -o build/bin/tcp_client tcp-client/main.go
go build -o build/bin/tun_client tun-client/main.go
go build -o build/bin/tun_server tun-server/main.go
go build -o build/bin/tcp_server tcp-server/main.go