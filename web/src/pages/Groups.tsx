import { FormEvent, useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { Group, Instance, api } from "../api";

export function GroupsPage() {
  const [groups, setGroups] = useState<Group[]>([]);
  const [instances, setInstances] = useState<Instance[]>([]);
  const [name, setName] = useState("");
  const [selected, setSelected] = useState<string[]>([]);
  const [error, setError] = useState("");

  async function load() {
    const [groupData, instanceData] = await Promise.all([
      api<{ items: Group[] }>("/api/v1/groups"),
      api<{ items: Instance[] }>("/api/v1/instances"),
    ]);
    setGroups(groupData.items ?? []);
    setInstances(instanceData.items ?? []);
  }

  useEffect(() => {
    load().catch((err: Error) => setError(err.message));
  }, []);

  const chosen = instances.filter((item) => selected.includes(item.id));
  const profileIds = new Set(chosen.map((item) => item.profileId));
  const sameProfile = chosen.length >= 2 && profileIds.size === 1;

  function toggle(id: string) {
    setSelected((current) => (current.includes(id) ? current.filter((item) => item !== id) : [...current, id]));
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    setError("");
    try {
      await api("/api/v1/groups", { method: "POST", body: JSON.stringify({ name, instanceIds: selected }) });
      setName("");
      setSelected([]);
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : "创建失败");
    }
  }

  return (
    <section>
      <header className="page-head">
        <h1>群控</h1>
        <p>一组只能放同一个档案。手势按每台当前画面尺寸换算，画面各自传输。</p>
      </header>
      <form className="stack" onSubmit={submit}>
        <input placeholder="组名" value={name} onChange={(event) => setName(event.target.value)} required />
        <ul className="pick-list">
          {instances.map((item) => (
            <li key={item.id}>
              <label className="check">
                <input type="checkbox" checked={selected.includes(item.id)} onChange={() => toggle(item.id)} />
                {item.name} · {item.profileId} · {item.status}
              </label>
            </li>
          ))}
          {instances.length === 0 ? <li>还没有云手机。</li> : null}
        </ul>
        <button type="submit" disabled={!sameProfile}>
          创建群组
        </button>
        {selected.length >= 2 && profileIds.size !== 1 ? <p className="error">所选云手机档案不一致。</p> : null}
      </form>
      {error ? <p className="error">{error}</p> : null}
      <table>
        <thead>
          <tr>
            <th>名称</th>
            <th>档案</th>
            <th>台数</th>
          </tr>
        </thead>
        <tbody>
          {groups.map((group) => (
            <tr key={group.id}>
              <td>
                <Link to={`/groups/${group.id}`}>{group.name}</Link>
              </td>
              <td>{group.profileId}</td>
              <td>{group.instanceIds.length}</td>
            </tr>
          ))}
          {groups.length === 0 ? (
            <tr>
              <td colSpan={3}>还没有群组。</td>
            </tr>
          ) : null}
        </tbody>
      </table>
    </section>
  );
}
