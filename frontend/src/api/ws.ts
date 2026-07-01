import axios from "axios";
import { useAuthStore } from "../stores/auth";

export interface UsageLogEntry {
  id: number;
  model: string;
  request_tokens: number;
  response_tokens: number;
  total_tokens: number;
  cache_write_5m_tokens: number;
  cache_write_1h_tokens: number;
  cache_read_tokens: number;
  latency_ms: number;
  cost: number;
  is_error: boolean;
  completed_at: string | null;
  created_at: string;
}

export interface StatsDelta {
  today_cost: number;
  today_requests: number;
  today_tokens: number;
  total_cost: number;
  total_requests: number;
  total_tokens: number;
  rpm: number;
  tpm: number;
  avg_latency_ms: number;
}

type EventHandler = (data: any) => void;

export interface RealtimeConnection {
  onUsageLog: (cb: (entry: UsageLogEntry) => void) => void;
  onStatsDelta: (cb: (delta: StatsDelta) => void) => void;
  close: () => void;
  isConnected: () => boolean;
}

export function createRealtimeConnection(): RealtimeConnection {
  const auth = useAuthStore();
  let ws: WebSocket | null = null;
  let reconnectDelay = 1000;
  let closed = false;
  let connected = false;

  const usageLogHandlers: EventHandler[] = [];
  const statsDeltaHandlers: EventHandler[] = [];

  function connect() {
    if (!auth.token || closed) return;

    const proto = window.location.protocol === "https:" ? "wss" : "ws";
    const url = `${proto}://${window.location.host}/admin/ws?token=${auth.token}`;
    ws = new WebSocket(url);

    ws.onopen = () => {
      connected = true;
      reconnectDelay = 1000;
      axios.get("/admin/stats").then((res) => {
        statsDeltaHandlers.forEach((cb) => cb(res.data));
      });
    };

    ws.onmessage = (event) => {
      try {
        const msg = JSON.parse(event.data);
        if (msg.type === "usage_log") {
          usageLogHandlers.forEach((cb) => cb(msg.data));
        } else if (msg.type === "stats_delta") {
          statsDeltaHandlers.forEach((cb) => cb(msg.data));
        }
      } catch (e) {
        console.warn("Failed to parse WS message:", e);
      }
    };

    ws.onclose = () => {
      connected = false;
      if (!closed) {
        setTimeout(connect, reconnectDelay);
        reconnectDelay = Math.min(reconnectDelay * 2, 30000);
      }
    };

    ws.onerror = () => {
      ws?.close();
    };
  }

  connect();

  return {
    onUsageLog(cb) {
      usageLogHandlers.push(cb);
    },
    onStatsDelta(cb) {
      statsDeltaHandlers.push(cb);
    },
    close() {
      closed = true;
      ws?.close();
      ws = null;
    },
    isConnected() {
      return connected;
    },
  };
}
