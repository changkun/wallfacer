// Browser RUM (real user monitoring) via OpenTelemetry. Spans are exported
// OTLP/HTTP to the same-origin /v1/telemetry/* route, which the backend
// (otel.TelemetryProxy) forwards to the in-cluster collector — browsers cannot
// reach the collector directly. Same-origin means no CORS and reuse of the
// page's existing TLS/session.
import { WebTracerProvider, BatchSpanProcessor } from '@opentelemetry/sdk-trace-web';
import { OTLPTraceExporter } from '@opentelemetry/exporter-trace-otlp-http';
import { resourceFromAttributes } from '@opentelemetry/resources';
import { ATTR_SERVICE_NAME } from '@opentelemetry/semantic-conventions';
import { ZoneContextManager } from '@opentelemetry/context-zone';
import { registerInstrumentations } from '@opentelemetry/instrumentation';
import { DocumentLoadInstrumentation } from '@opentelemetry/instrumentation-document-load';
import { FetchInstrumentation } from '@opentelemetry/instrumentation-fetch';
import { XMLHttpRequestInstrumentation } from '@opentelemetry/instrumentation-xml-http-request';

// ignore the telemetry POST itself so the exporter does not instrument its own
// requests.
const ignoreTelemetry = [/\/v1\/telemetry\//];

// initTelemetry wires document-load, fetch, and XHR instrumentation and exports
// spans to the backend telemetry proxy. serviceName labels the browser spans
// (e.g. "lux-web") so they are distinct from the server service in dashboards.
export function initTelemetry(serviceName: string): void {
  const exporter = new OTLPTraceExporter({ url: '/v1/telemetry/v1/traces' });
  const provider = new WebTracerProvider({
    resource: resourceFromAttributes({ [ATTR_SERVICE_NAME]: serviceName }),
    spanProcessors: [new BatchSpanProcessor(exporter)],
  });
  provider.register({ contextManager: new ZoneContextManager() });

  registerInstrumentations({
    instrumentations: [
      new DocumentLoadInstrumentation(),
      // No propagateTraceHeaderCorsUrls: trace headers stay same-origin by
      // default, linking browser spans to our backend spans without leaking
      // traceparent to third-party hosts.
      new XMLHttpRequestInstrumentation({ ignoreUrls: ignoreTelemetry }),
      new FetchInstrumentation({ ignoreUrls: ignoreTelemetry }),
    ],
  });
}
