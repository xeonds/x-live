build: x-live docker-compose.yml nginx.conf
	@mkdir -p build
	cp docker-compose.yml nginx.conf build/

x-live: main.go go.mod go.sum
	go build -o build/x-live
