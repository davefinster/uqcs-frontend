buildProto:
	protoc -I proto/ proto/server.proto --go_out=plugins=grpc:./frontend --go_opt=module=github.com/davefinster/uqcs-demo/backend