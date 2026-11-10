import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { api, setToken } from "../api";
import {
  AuthenticationImage,
  LoginValues,
} from "../components/AuthenticationImage/AuthenticationImage";

export function LoginPage() {
  const navigate = useNavigate();
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  async function submit(values: LoginValues) {
    setError("");
    setLoading(true);
    try {
      const session = await api<{ accessToken: string }>("/api/v1/auth/login", {
        method: "POST",
        body: JSON.stringify(values),
      });
      setToken(session.accessToken);
      navigate("/instances");
    } catch (err) {
      setError(err instanceof Error ? err.message : "登录失败");
      setLoading(false);
    }
  }

  return <AuthenticationImage defaultUsername="admin" loading={loading} error={error} onSubmit={submit} />;
}
