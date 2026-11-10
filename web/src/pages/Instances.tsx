import { FormEvent, useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { Host, Instance, Profile, api } from "../api";

export function InstancesPage() {
  const [items, setItems] = useState<Instance[]>([]);
  const [hosts, setHosts] = useState<Host[]>([]);
  const [profiles, setProfiles] = useState<Profile[]>([]);
  const [name, setName] = useState("");
  const [hostId, setHostId] = useState("");
  const [profileId, setProfileId] = useState("");
  const [proxy, setProxy] = useState("");
  const [adbSerial, setAdbSerial] = useState("");
  const [error, setError] = useState("");

  async function load() {
    const [instanceData, hostData, profileData] = await Promise.all([
      api<{ items: Instance[] }>("/api/v1/instances"),
      api<{ items: Host[] }>("/api/v1/hosts"),
      api<{ items: Profile[] }>("/api/v1/profiles"),
    ]);
    setItems(instanceData.items ?? []);
    setHosts(hostData.items ?? []);
    setProfiles(profileData.items ?? []);
    setHostId((current) => current || hostData.items?.[0]?.id || "");
    setProfileId((current) => current || profileData.items?.[0]?.id || "");
  }

  useEffect(() => {
    load().catch((err: Error) => setError(err.message));
    const timer = window.setInterval(() => {
      load().catch(() => undefined);
    }, 3000);
    return () => window.clearInterval(timer);
  }, []);

  function changeAdb(value: string) {
    const wasEmpty = adbSerial.trim() === "";
    const nowEmpty = value.trim() === "";
    setAdbSerial(value);
    if (wasEmpty && !nowEmpty) setProfileId("");
    if (!wasEmpty && nowEmpty) setProfileId((current) => current || profiles[0]?.id || "");
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    setError("");
    try {
      await api("/api/v1/instances", {
        method: "POST",
        body: JSON.stringify({ name, hostId, profileId, httpProxy: proxy, adbSerial }),
      });
      setName("");
      setAdbSerial("");
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : "创建失败");
    }
  }

  return (
    <section>
      <header className="page-head">
        <h1>云手机</h1>
        <p>留空 ADB 地址会在节点上新建容器。填了地址则接入这台已有设备，不新建容器，也不改它的机型。</p>
      </header>
      <form className="row" onSubmit={submit}>
        <input placeholder="名称" value={name} onChange={(event) => setName(event.target.value)} required />
        <select value={hostId} onChange={(event) => setHostId(event.target.value)}>
          {hosts.map((host) => (
            <option key={host.id} value={host.id}>
              {host.name} · {host.status} · binderfs {host.binderfs}
            </option>
          ))}
        </select>
        <select value={profileId} onChange={(event) => setProfileId(event.target.value)}>
          {adbSerial.trim() ? <option value="">不套用档案</option> : null}
          {profiles.map((profile) => (
            <option key={profile.id} value={profile.id}>
              {profile.model} {profile.width}×{profile.height}
            </option>
          ))}
        </select>
        <input placeholder="HTTP 代理，可空" value={proxy} onChange={(event) => setProxy(event.target.value)} />
        <input placeholder="ADB 地址，如 192.168.1.8:5555" value={adbSerial} onChange={(event) => changeAdb(event.target.value)} />
        <button type="submit">{adbSerial.trim() ? "接入" : "创建"}</button>
      </form>
      {error ? <p className="error">{error}</p> : null}
      <table>
        <thead>
          <tr>
            <th>名称</th>
            <th>状态</th>
            <th>档案</th>
            <th>ADB</th>
            <th>宿主</th>
          </tr>
        </thead>
        <tbody>
          {items.map((item) => (
            <tr key={item.id}>
              <td>
                <Link to={`/instances/${item.id}`}>{item.name}</Link>
              </td>
              <td><span className={`status ${item.status}`}>{item.status}</span></td>
              <td>{item.profileId}</td>
              <td>{item.adbSerial || item.adbPort || "—"}</td>
              <td>{hosts.find((host) => host.id === item.hostId)?.name || item.hostId}</td>
            </tr>
          ))}
          {items.length === 0 ? (
            <tr>
              <td colSpan={5}>还没有云手机。</td>
            </tr>
          ) : null}
        </tbody>
      </table>
    </section>
  );
}
