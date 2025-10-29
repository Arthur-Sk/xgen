go-update:
  go get -u -t ./... && just go-tidy

go-tidy:
  go mod tidy

go-build:
  go build ./cmd/xgen/xgen.go

xsd-gen:
  go run cmd/xgen/xgen.go -p out -i source/common_types.xsd -o out/commonTypes.go -l Go
  go run cmd/xgen/xgen.go -p out -i source/train_operation.xsd -o out/trainOperation.go -l Go
