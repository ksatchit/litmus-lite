# Cookbook: HTTP latency

```
go build -o litmus-lite ./cmd/litmus-lite
./litmus-lite hub import -beside . http.latency
# edit listen/upstream in the file
./litmus-lite validate ./http-latency.chaos.yaml
./litmus-lite run ./http-latency.chaos.yaml -out run.json -html run.html
./litmus-lite diagnose run.json
```

Point probes (and VegaLoad) at `params.listen`, not `upstream`.
