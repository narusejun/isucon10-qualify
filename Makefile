PROJECT_ROOT:=/home/isucon/isuumo
BUILD_DIR:=/home/isucon/isuumo/webapp/go
BIN_NAME:=isuumo
BIN_PATH:=/home/isucon/isuumo/webapp/go/isuumo
SERVICE_NAME:=isuumo.go
APP_LOCAL_URL:=http://localhost:1323

NGX_SERVICE=nginx
NGX_LOG:=/var/log/nginx/access.log

MYSQL_SERVICE=mysql
MYSQL_LOG:=/var/log/mysql/mysql.log

HOSTNAME:=$(shell hostname)

all: build

.PHONY: clean
clean:
	cd $(BUILD_DIR); \
	rm -rf ${BIN_NAME}

.PHONY: deploy
deploy: before build config-files start

.PHONY: deploy-nolog
deploy-nolog: before build-nolog config-files start

.PHONY: build
build:
	git pull&& \
	cd $(BUILD_DIR); \
	go build -o isuumo
	# TODO

.PHONY: build-nolog
build-nolog:
	git pull&& \
	cd $(BUILD_DIR); \
	go build -tags release -o isuumo
	# TODO

.PHONY: config-files
config-files:
	sudo rsync -v -r $(HOSTNAME)/ /

.PHONY: start
start:
	sh $(HOSTNAME)/deploy.sh

.PHONY: pprof
pprof:
	pprof -png -output /tmp/pprof.png $(BIN_PATH) $(APP_LOCAL_URL)/debug/pprof/profile
	# slackcat /tmp/pprof.png
	pprof -http=0.0.0.0:9090 $(BIN_PATH) `ls -lt $(HOME)/pprof/* | head -n 1 | gawk '{print $$9}'`

.PHONY: kataru
kataru:
	sudo cat $(NGX_LOG) | kataribe -f /etc/kataribe.toml | slackcat

.PHONY: before
before:
	$(eval when := $(shell date "+%s"))
	mkdir -p ~/logs/$(when)
	sudo mv -f $(NGX_LOG) ~/logs/$(when)/
	sudo mv -f $(MYSQL_LOG) ~/logs/$(when)/
