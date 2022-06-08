all:
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o OneAuth-Agent.exe
	go build -o OneAuth-Agent
