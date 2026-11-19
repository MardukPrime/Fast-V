import { render } from "solid-js/web";

import App from "./App";
import FloatBall from "./pages/FloatBall";
import "./styles.css";

const root = document.getElementById("root");

if (!root) {
  throw new Error("Root element not found");
}

// The floating traffic ball lives in a separate frameless window created with
// the "?float=1" query parameter; it renders only the ball, not the app shell.
const isFloatWindow = new URLSearchParams(window.location.search).get("float") === "1";

render(() => (isFloatWindow ? <FloatBall /> : <App />), root);
