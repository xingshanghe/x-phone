import { FormEvent, useEffect, useState } from "react";
import { Host, api } from "../api";

export function HostsPage() {
  const [items, setItems] = useState<Host[]>([]);
  const [name, setName] = useState("");
  const [addr, setAddr] = useState("127.0.0.1");
  const [capacity, setCapacity] = useState(4);
  const [token, setAgentToken] = useState("");
  const [error, setError] = useState("");

  async function load() {
    const data = await api<{ items: Host[] }>("/api/v1/hosts");
    setItems(data.items ?? []);
  }

  useEffect(() => {
    load().catch((err: Error) => setError(err.message));
  }, []);

  async function submit(event: FormEvent) {
    event.preventDefault();
    setError("");
    try {
      const created = await api<{ agentToken: string }>("/api/v1/hosts", {
        method: "POST",
        body: JSON.stringify({ name, addr, capacity }),
      });
      setAgentToken(created.agentToken);
      setName("");
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : "登记失败");
    }
  }

  return (
    <section>
      <header className="page-head">
        <h1>节点</h1>
        <p>每台 Linux 宿主跑一个 agent。心跳超过 45 秒会标成离线。</p>
      </header>
      <form className="row" onSubmit={submit}>
        <input placeholder="名称" value={name} onChange={(event) => setName(event.target.value)} required />
        <input placeholder="地址" value={addr} onChange={(event) => setAddr(event.target.value)} required />
        <input type="number" min={1} value={capacity} onChange={(event) => setCapacity(Number(event.target.value))} />
        <button type="submit">登记</button>
      </form>
      {token ? <p className="token">Agent token（只显示一次）：{token}</p> : null}
      {error ? <p className="error">{error}</p> : null}
      <table>
        <thead>
          <tr>
            <th>名称</th>
            <th>地址</th>
            <th>状态</th>
            <th>binderfs</th>
            <th>容量</th>
            <th>运行中</th>
          </tr>
        </thead>
        <tbody>
          {items.map((host) => (
            <tr key={host.id}>
              <td>{host.name}</td>
              <td>{host.addr}</td>
              <td><span className={`status ${host.status}`}>{host.status}</span></td>
              <td>{host.binderfs}</td>
              <td>{host.capacity}</td>
              <td>{host.runningCount}</td>
            </tr>
          ))}
          {items.length === 0 ? (
            <tr>
              <td colSpan={6}>还没有节点。</td>
            </tr>
          ) : null}
        </tbody>
      </table>
    </section>
  );
}
