import { Alert, Button, Paper, PasswordInput, Text, TextInput, Title } from "@mantine/core";
import { FormEvent } from "react";
import classes from "./AuthenticationImage.module.css";

export type LoginValues = { username: string; password: string };

export type AuthenticationImageProps = {
  defaultUsername?: string;
  loading?: boolean;
  error?: string;
  onSubmit: (values: LoginValues) => void;
};

export function AuthenticationImage({
  defaultUsername = "",
  loading = false,
  error,
  onSubmit,
}: AuthenticationImageProps) {
  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    onSubmit({
      username: String(form.get("username") ?? "").trim(),
      password: String(form.get("password") ?? ""),
    });
  }

  return (
    <div className={classes.wrapper}>
      <Paper className={classes.form} radius={0}>
        <form onSubmit={handleSubmit}>
          <Text className={classes.eyebrow}>Android cloud phone</Text>
          <Title order={2} className={classes.title}>
            进入机房
          </Title>

          <TextInput
            label="账号"
            name="username"
            placeholder="请输入账号"
            defaultValue={defaultUsername}
            autoComplete="username"
            size="md"
            radius="md"
            required
          />
          <PasswordInput
            label="密码"
            name="password"
            placeholder="请输入密码"
            autoComplete="current-password"
            mt="md"
            size="md"
            radius="md"
            required
          />

          {error ? (
            <Alert color="red" variant="light" radius="md" mt="md" title="登录失败">
              {error}
            </Alert>
          ) : null}

          <Button type="submit" fullWidth mt="xl" size="md" radius="md" loading={loading}>
            登录
          </Button>

          <Text ta="center" mt="md" size="xs" c="dimmed">
            x-phone · 云手机控制台
          </Text>
        </form>
      </Paper>
    </div>
  );
}
