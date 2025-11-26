.PHONY: proto-go proto-python build run-gateway run-worker

# Generate Go protobuf code
proto-go:
	protoc -Iinference/proto \
	--go_out=inference/pb --go_opt=paths=source_relative \
	--go-grpc_out=inference/pb --go-grpc_opt=paths=source_relative \
	inference.proto

# Generate Python protobuf code
proto-python:
	python3 -m grpc_tools.protoc -Iinference/proto \
	--python_out=workers --grpc_python_out=workers \
	inference.proto

# Build the Go Gateway
build:
	go build -o bin/gateway cmd/gateway/main.go

# Run the Go Gateway
run-gateway:
	./bin/gateway

# Run the Python Worker (Uses Hugging Face Transformers)
run-worker:
	python3 workers/server.py --model gpt2 --port 50051 --device cpu
