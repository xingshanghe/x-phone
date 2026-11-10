import { FormEvent, PointerEvent, useEffect, useRef, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { Group, Instance, api, wsURL } from "../api";

type Point = { xr: number; yr: number; at: number };

export function GroupSessionPage() {
  const { id = "" } = useParams();
  const [group, setGroup] = useState<Group | null>(null);
  const [instances, setInstances] = useState<Instance[]>([]);
  const [leaderId, setLeaderId] = useState("");
  const [note, setNote] = useState("");
  const [error, setError] = useState("");
  const [text, setText] = useState("");
  const controlRef = useRef<WebSocket | null>(null);
  const originRef = useRef<Point | null>(null);

  useEffect(() => {
    Promise.all([api<Group>(`/api/v1/groups/${id}`), api<{ items: Instance[] }>("/api/v1/instances")])
      .then(([item, instanceData]) => {
        setGroup(item);
        setInstances(instanceData.items ?? []);
        setLeaderId((current) => current || item.instanceIds[0] || "");
      })
      .catch((err: Error) => setError(err.message));
  }, [id]);

  useEffect(() => {
    if (!id) return;
    const socket = new WebSocket(wsURL(`/api/v1/groups/${id}/control`));
    let closed = false;
    controlRef.current = socket;
    socket.onmessage = (event) => {
      if (closed) return;
      const message = JSON.parse(event.data as string) as { type?: string; text?: string; delivered?: number; offline?: number };
      if (message.type === "status") setNote(message.text || "控制失败");
      if (message.type === "fanout") setNote(`已送达 ${message.delivered ?? 0} 台，离线 ${message.offline ?? 0} 台`);
    };
    socket.onerror = () => {
      if (!closed) setNote("控制连接失败");
    };
    return () => {
      closed = true;
      controlRef.current = null;
      socket.close();
    };
  }, [id]);

  const members = group?.instanceIds ?? [];
  const leader = leaderId || members[0] || "";
  const followers = members.filter((item) => item !== leader);

  function send(body: Record<string, unknown>) {
    const socket = controlRef.current;
    if (!socket || socket.readyState !== WebSocket.OPEN) {
      setNote("控制连接还没就绪");
      return;
    }
    socket.send(JSON.stringify(body));
  }

  function pointerDown(event: PointerEvent<HTMLImageElement>) {
    const point = relativePoint(event);
    if (!point) return;
    originRef.current = { ...point, at: event.timeStamp };
    event.currentTarget.setPointerCapture(event.pointerId);
  }

  function pointerUp(event: PointerEvent<HTMLImageElement>) {
    const origin = originRef.current;
    originRef.current = null;
    const point = relativePoint(event);
    if (!origin || !point) return;
    const moved = Math.hypot(point.xr - origin.xr, point.yr - origin.yr);
    if (moved < 0.02) {
      send({ type: "tap", xr: point.xr, yr: point.yr });
      return;
    }
    const duration = Math.min(1000, Math.max(100, Math.round(event.timeStamp - origin.at)));
    send({ type: "swipe", xr: origin.xr, yr: origin.yr, x2r: point.xr, y2r: point.yr, duration });
  }

  async function batch(type: "start" | "stop") {
    setError("");
    try {
      await api(`/api/v1/groups/${id}/batch`, { method: "POST", body: JSON.stringify({ type }) });
      setNote(type === "start" ? "已展开启动任务" : "已展开停止任务");
    } catch (err) {
      setError(err instanceof Error ? err.message : "批量操作失败");
    }
  }

  function submitText(event: FormEvent) {
    event.preventDefault();
    const value = text.trim();
    if (!value) return;
    send({ type: "text", text: value });
    setText("");
  }

  return (
    <section>
      <header className="page-head">
        <h1>{group?.name || "群控"}</h1>
        <p>
          <Link to="/groups">返回群组</Link>
          {group ? ` · ${group.profileId} · ${members.length} 台` : ""}
        </p>
      </header>
      <div className="actions">
        <label>
          主控
          <select value={leader} onChange={(event) => setLeaderId(event.target.value)}>
            {members.map((item) => (
              <option key={item} value={item}>
                {instances.find((instance) => instance.id === item)?.name || item}
              </option>
            ))}
          </select>
        </label>
        <button type="button" onClick={() => batch("start")}>
          全部启动
        </button>
        <button type="button" className="ghost" onClick={() => batch("stop")}>
          全部停止
        </button>
        <button type="button" className="ghost" onClick={() => send({ type: "key", key: "HOME" })}>
          Home
        </button>
        <button type="button" className="ghost" onClick={() => send({ type: "key", key: "BACK" })}>
          Back
        </button>
      </div>
      <form className="row" onSubmit={submitText}>
        <input placeholder="向全组输入文本" value={text} onChange={(event) => setText(event.target.value)} />
        <button type="submit">发送</button>
      </form>
      {error ? <p className="error">{error}</p> : null}
      {note ? <p className="token">{note}</p> : null}
      <div className="group-layout">
        <div className="screen leader-screen">
          {leader ? (
            <FrameView id={leader} onPointerDown={pointerDown} onPointerUp={pointerUp} />
          ) : (
            <p>这个组还没有设备。</p>
          )}
        </div>
        <div className="follower-grid">
          {followers.map((item) => (
            <FrameView key={item} id={item} compact />
          ))}
        </div>
      </div>
    </section>
  );
}

function FrameView({
  id,
  compact = false,
  onPointerDown,
  onPointerUp,
}: {
  id: string;
  compact?: boolean;
  onPointerDown?: (event: PointerEvent<HTMLImageElement>) => void;
  onPointerUp?: (event: PointerEvent<HTMLImageElement>) => void;
}) {
  const [frame, setFrame] = useState("");
  const [note, setNote] = useState("等待画面");

  useEffect(() => {
    const socket = new WebSocket(wsURL(`/api/v1/instances/${id}/stream`));
    let closed = false;
    socket.onmessage = (event) => {
      if (closed) return;
      const message = JSON.parse(event.data as string) as { type?: string; mime?: string; data?: string; text?: string };
      if (message.type === "frame" && message.mime && message.data) {
        setFrame(`data:${message.mime};base64,${message.data}`);
        setNote("");
      }
      if (message.type === "status" && message.text) setNote(message.text);
    };
    socket.onerror = () => {
      if (!closed) setNote("画面连接失败");
    };
    return () => {
      closed = true;
      socket.close();
    };
  }, [id]);

  return (
    <div className={compact ? "tile" : "leader-frame"}>
      {frame ? (
        <img
          src={frame}
          alt="云手机画面"
          onPointerDown={onPointerDown}
          onPointerUp={onPointerUp}
          draggable={false}
        />
      ) : (
        <p>{note}</p>
      )}
    </div>
  );
}

function relativePoint(event: PointerEvent<HTMLImageElement>): Point | null {
  const rect = event.currentTarget.getBoundingClientRect();
  if (rect.width === 0 || rect.height === 0) return null;
  return {
    xr: clamp((event.clientX - rect.left) / rect.width),
    yr: clamp((event.clientY - rect.top) / rect.height),
    at: event.timeStamp,
  };
}

function clamp(value: number): number {
  return Math.min(1, Math.max(0, value));
}
