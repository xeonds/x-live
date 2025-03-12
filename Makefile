build: x-live docker-compose.yml nginx.conf index.html
	@mkdir -p build/static
	cp index.html docker-compose.yml nginx.conf build/

x-live: main.go go.mod go.sum
	go build -o build/x-live
