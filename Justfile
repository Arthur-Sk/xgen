go-update:
  go get -u -t ./... && just go-tidy

go-tidy:
  go mod tidy

go-build:
  go build ./cmd/xgen/xgen.go

xsd-gen:
  go run cmd/xgen/xgen.go -p output -i data/go/source/common_types.xsd -o data/go/output/commonTypes.go -l Go -omit-xmlname
  go run cmd/xgen/xgen.go -p output -i data/go/source/train_operation.xsd -o data/go/output/trainOperation.go -l Go -omit-xmlname
