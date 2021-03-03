export GOOS=linux
go build -o sample/bin/tcp_client sample/tcp-client/main.go
go build -o sample/bin/tun_client sample/tun-client/main.go
go build -o sample/bin/tun_server sample/tun-server/main.go
go build -o sample/bin/tcp_server sample/tcp-server/main.go
go build -o sample/bin/tcp_file_client sample/tcp-file-client/main.go
go build -o sample/bin/tcp_file_server sample/tcp-file-server/main.go
go build -o sample/bin/udp-client sample/udp-client/main.go
go build -o sample/bin/udp-server sample/udp-server/main.go
export GOOS=