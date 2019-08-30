set GOROOT=c:\go
set GOOS=windows

set GODEBUG=cgocheck=0

set GOARCH=amd64
set CGO_ENABLED=1
set CGO_CFLAGS=-Id:\oracle\instantclient_12_1\sdk\include
set CGO_LDFLAGS=-Ld:\oracle\instantclient_12_1\sdk\lib -loci
set PKG_CONFIG_PATH=d:\oracle\instantclient_12_1\
set LD_LIBRARY_PATH=d:\oracle\instantclient_12_1\
go build -ldflags "-s -w" -v -o 64/iplsgo.exe

rem Version GOARCH=386 not support
rem set GOARCH=386
rem set CGO_ENABLED=1
rem set CGO_CFLAGS=-Id:\oracle\instantclient_12_2.32\sdk\include
set CGO_LDFLAGS=-Ld:\oracle\instantclient_12_2.32\sdk\lib -loci
rem rem set PKG_CONFIG_PATH=d:\oracle\instantclient_12_2.32\
rem set LD_LIBRARY_PATH=d:\oracle\instantclient_12_2.32\
rem go build -ldflags "-s -w" -i -v -o 32/iplsgo.exe