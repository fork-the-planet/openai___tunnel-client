export interface TextResponse {
  ok: boolean;
  status: number;
  text: string;
}

export async function fetchJSON<T>(path: string): Promise<T> {
  const res = await fetch(path, { cache: "no-store" });
  if (!res.ok) {
    throw new Error(`${res.status} ${res.statusText}`);
  }
  return (await res.json()) as T;
}

export async function fetchTextResponse(path: string): Promise<TextResponse> {
  const res = await fetch(path, { cache: "no-store" });
  return {
    ok: res.ok,
    status: res.status,
    text: await res.text(),
  };
}

export async function fetchText(path: string): Promise<string> {
  const res = await fetchTextResponse(path);
  if (!res.ok) {
    throw new Error(`${res.status}`);
  }
  return res.text;
}
