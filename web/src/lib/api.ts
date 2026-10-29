export async function api<T>(
  path: string,
  options: RequestInit = {},
): Promise<T> {
  const r = await fetch("/api" + path, {
    credentials: "same-origin",
    ...options,
    headers: { "Content-Type": "application/json", ...options.headers },
  });
  const data = await r.json();
  if (!r.ok)
    throw new Error(r.status === 401 ? "AUTH" : data.error || "请求失败");
  return data;
}
export function elapsed(value: string) {
  const n = Date.now() - new Date(value).getTime();
  if (n < 60000) return "刚刚";
  if (n < 3600000) return `${Math.floor(n / 60000)} 分钟前`;
  if (n < 86400000) return `${Math.floor(n / 3600000)} 小时前`;
  return new Date(value).toLocaleDateString("zh-CN");
}
