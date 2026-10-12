# Cashier

Small Go service for the Mode B failpoint demo. One route, `GET /charge`, calls `failpoint.Delay` and `failpoint.Fail`. Both stay dark until a scenario arms them.

This process is separate from LaunchPad. Walkthrough: [demo playbook](../../docs/cookbooks/demo-playbook-failpoint.md).

```
export LITMUS_LITE_FAILPOINT=charge,charge-slow
export LITMUS_LITE_FAILPOINT_TOKEN=cashier-demo
go run ./examples/cashier
```

Listens on `127.0.0.1:8091`.
