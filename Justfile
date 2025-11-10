go-update:
  go get -u -t ./... && just go-tidy

go-tidy:
  go mod tidy

go-build:
  go build ./cmd/xgen/xgen.go

go-vet:
  go vet ./data/go/output/...

xsd-gen:
  go run cmd/xgen/xgen.go -p output -i data/go/source/common_types.xsd -o data/go/output/commonType.go -l Go
  go run cmd/xgen/xgen.go -p output -i data/go/source/train_operation.xsd -o data/go/output/trainOperation.go -l Go
