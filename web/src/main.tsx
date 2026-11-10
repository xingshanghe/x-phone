import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import { MantineProvider, colorsTuple, createTheme } from "@mantine/core";
import "@mantine/core/styles.css";
import { App } from "./App";
import "./styles.css";

const theme = createTheme({
  primaryColor: "xphone",
  colors: {
    xphone: colorsTuple([
      "#f4ffd9",
      "#e9ffab",
      "#ddff7a",
      "#d2ff45",
      "#c3f521",
      "#a8dd0c",
      "#8bc408",
      "#6eaa05",
      "#539203",
      "#3c7902",
    ]),
  },
  fontFamily: '"Avenir Next", "Segoe UI", sans-serif',
  defaultRadius: "md",
});

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <MantineProvider theme={theme} defaultColorScheme="dark">
      <BrowserRouter>
        <App />
      </BrowserRouter>
    </MantineProvider>
  </StrictMode>,
);
