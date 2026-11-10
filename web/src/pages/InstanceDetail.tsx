import { FormEvent, MouseEvent, useEffect, useRef, useState } from "react";
import { useParams } from "react-router-dom";
import { Instance, ProfileReport, Task, api, wsURL } from "../api";

export function InstanceDetailPage() {
  const { id = "" } = useParams();
  const [item, setItem] = useState<Instance | null>(null);
  const [tasks, setTasks] = useState<Task[]>([]);
  const [frame, setFrame] = useState("");
  const [note, setNote] = useState("等待画面");
  const [packageName, setPackageName] = useState("");
  const [error, setError] = useState("");
  const streamRef = useRef<WebSocket | null>(null);

  async function load() {
    const [instance, taskData] = await Promise.all([
      api<Instance>(`/api/v1/instances/${id}`),
      api<{ items: Task[] }>(`/api/v1/instances/${id}/tasks`),
    ]);
    setItem(instance);
    setTasks(taskData.items ?? []);
  }

  useEffect(() => {
    load().catch((err: Error) => setError(err.message));
    const timer = window.setInterval(() => {
      load().catch(() => undefined);
    }, 2000);
    return () => window.clearInterval(timer);
  }, [id]);

  useEffect(() => {
    if (!id || item?.status !== "running") return;
    const socket = new WebSocket(wsURL(`/api/v1/instances/${id}/stream`));
    let closed = false;
    streamRef.current = socket;
    socket.onmessage = (event) => {
      if (closed) return;
      const message = JSON.parse(event.data as string) as { type: string; mime?: string; data?: string; text?: string };
      if (message.type === "frame" && message.mime && message.data) {
        setFrame(`data:${message.mime};base64,${message.data}`);
        setNote("");
      }
      if (message.type === "status") setNote(message.text || "画面中断");
    };
    socket.onerror = () => {
      if (!closed) setNote("画面连接失败");
    };
    return () => {
      closed = true;
      streamRef.current = null;
      socket.close();
    };
  }, [id, item?.status]);

  async function act(action: "start" | "stop" | "delete") {
    setError("");
    try {
      await api(`/api/v1/instances/${id}${action === "delete" ? "" : `/${action}`}`, { method: action === "delete" ? "DELETE" : "POST" });
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : "操作失败");
    }
  }

  async function upload(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    form.set("packageName", packageName);
    setError("");
    try {
      await api(`/api/v1/instances/${id}/apk`, { method: "POST", body: form });
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : "安装失败");
    }
  }

  function tap(event: MouseEvent<HTMLImageElement>) {
    const rect = event.currentTarget.getBoundingClientRect();
    if (rect.width === 0 || rect.height === 0) return;
    const xr = (event.clientX - rect.left) / rect.width;
    const yr = (event.clientY - rect.top) / rect.height;
    const socket = streamRef.current;
    if (!socket || socket.readyState !== WebSocket.OPEN) return;
    socket.send(JSON.stringify({ type: "tap", xr, yr }));
  }

  const report = parseReport(item?.profileReport);
  return (
    <section className="detail">
      <header className="page-head">
        <h1>{item?.name || "云手机"}</h1>
        <p>{item ? `${item.profileId} · ${item.status} · adb ${item.adbPort || "—"}` : "加载中"}</p>
      </header>
      <div className="actions">
        <button onClick={() => act("start")}>启动</button>
        <button onClick={() => act("stop")}>停止</button>
        <button className="danger" onClick={() => act("delete")}>删除</button>
      </div>
      {error ? <p className="error">{error}</p> : null}
      {item?.lastError ? <p className="error">{item.lastError}</p> : null}
      <div className="detail-grid">
        <div className="screen">
          {frame ? <img src={frame} alt="云手机画面" width={360} height={640} onClick={tap} /> : <p>{note}</p>}
        </div>
        <div>
          <h2>定制回读</h2>
          <ul className="steps">
            {(report?.steps ?? []).map((step) => (
              <li key={step.name}>
                <span className={step.ok ? "ok" : "bad"}>{step.ok ? "通过" : "失败"}</span>
                {step.name}
                <small>{step.detail}</small>
              </li>
            ))}
          </ul>
          <h2>尚未覆盖</h2>
          <p>{(report?.gaps ?? ["baseband", "play_integrity", "camera", "gps"]).join("、")}</p>
          <h2>安装 APK</h2>
          <form className="stack" onSubmit={upload}>
            <input placeholder="包名，例如 com.example.app" value={packageName} onChange={(event) => setPackageName(event.target.value)} required />
            <input name="file" type="file" accept=".apk" required />
            <button type="submit">安装</button>
          </form>
          <h2>任务</h2>
          <ul className="tasks">
            {tasks.map((task) => (
              <li key={task.id}>
                <strong>{task.type}</strong> <span className={`status ${task.status}`}>{task.status}</span>
                {task.log ? <pre>{task.log}</pre> : null}
              </li>
            ))}
            {tasks.length === 0 ? <li>还没有任务。</li> : null}
          </ul>
        </div>
      </div>
    </section>
  );
}

function parseReport(raw?: string): ProfileReport | null {
  if (!raw) return null;
  try {
    return JSON.parse(raw) as ProfileReport;
  } catch {
    return null;
  }
}
