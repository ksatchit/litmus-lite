// VegaLoad scenario for LaunchPad.
// Hit the litmus-lite proxy listen address (default :18080) so load sees the fault.
export default function () {
  http.get("http://127.0.0.1:18080/healthz");
  http.get("http://127.0.0.1:18080/api/launches");
}
