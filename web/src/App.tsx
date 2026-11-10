import { NavLink, Navigate, Route, Routes, useLocation, useNavigate } from "react-router-dom";
import { getToken, setToken } from "./api";
import { GroupSessionPage } from "./pages/GroupSession";
import { GroupsPage } from "./pages/Groups";
import { HostsPage } from "./pages/Hosts";
import { InstanceDetailPage } from "./pages/InstanceDetail";
import { InstancesPage } from "./pages/Instances";
import { LoginPage } from "./pages/Login";

export function App() {
  useLocation();
  const authed = Boolean(getToken());
  return (
    <Routes>
      <Route path="/login" element={authed ? <Navigate to="/instances" replace /> : <LoginPage />} />
      <Route path="/*" element={authed ? <Shell /> : <Navigate to="/login" replace />} />
    </Routes>
  );
}

function Shell() {
  const navigate = useNavigate();
  return (
    <div className="shell">
      <aside>
        <div className="brand">x-phone</div>
        <nav>
          <NavLink to="/instances">云手机</NavLink>
          <NavLink to="/groups">群控</NavLink>
          <NavLink to="/hosts">节点</NavLink>
        </nav>
        <button
          className="ghost"
          onClick={() => {
            setToken("");
            navigate("/login");
          }}
        >
          退出
        </button>
      </aside>
      <main>
        <Routes>
          <Route path="/" element={<Navigate to="/instances" replace />} />
          <Route path="/instances" element={<InstancesPage />} />
          <Route path="/instances/:id" element={<InstanceDetailPage />} />
          <Route path="/groups" element={<GroupsPage />} />
          <Route path="/groups/:id" element={<GroupSessionPage />} />
          <Route path="/hosts" element={<HostsPage />} />
        </Routes>
      </main>
    </div>
  );
}
