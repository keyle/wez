default:
	go build .

release: clean
	go build -ldflags "-s -w"

test: clean
	go test -v .

run:
	go run .

all: default test release install 

clean:
	rm -f ./wez
	go clean

install: release
	mkdir -p /usr/local/bin
	install -m755 wez /usr/local/bin/.
	mkdir -p /usr/local/share/man/man1
	install -m644 docs/wez.1 /usr/local/share/man/man1/.

docs:
	scdoc < docs/wez.scd > docs/wez.1

html: docs
	mandoc -T html docs/wez.1 > docs/wez.html.tmp
	awk '{gsub(/<\/head>/,"<style>html{background: #333; color: #eee; max-width: 50rem; margin: 1rem auto; font-family: sans-serif;} a{color: white;}<\/style><\/head>")}1' docs/wez.html.tmp > docs/wez.html
	$(RM) docs/wez.html.tmp
