.PHONY: run build clean

run:
	@mkdir -p tmp
	@set -a; . ./.env; set +a; exec go run .

build:
	go build .

clean:
	rm -f adprofd
