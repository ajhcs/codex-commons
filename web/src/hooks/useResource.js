import { useEffect, useState } from "react";

export function useResource(loader, dependencies) {
  const [state, setState] = useState({ status: "loading", data: null, error: null });

  useEffect(() => {
    const controller = new AbortController();
    setState((current) => ({ status: "loading", data: current.data, error: null }));
    loader(controller.signal)
      .then((data) => setState({ status: "ready", data, error: null }))
      .catch((error) => {
        if (error.name !== "AbortError") setState({ status: "error", data: null, error: error.message });
      });
    return () => controller.abort();
  }, dependencies);

  return state;
}
