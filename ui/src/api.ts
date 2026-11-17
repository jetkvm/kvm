function api(url: string, options: RequestInit): Promise<Response> {
  const baseOptions: RequestInit = {
    mode: "cors",
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
    },
    ...options,
  };

  return fetch(url, baseOptions);
}

export default Object.assign(api, {
  GET: (url: string, options?: RequestInit) => api(url, { method: "GET", ...options }),
  POST: (url: string, body?: object, options?: RequestInit) =>
    api(url, { method: "POST", body: JSON.stringify(body), ...options }),
  PUT: (url: string, body?: object, options?: RequestInit) =>
    api(url, { method: "PUT", body: JSON.stringify(body), ...options }),
  DELETE: (url: string, body?: object, options?: RequestInit) =>
    api(url, { method: "DELETE", body: JSON.stringify(body), ...options }),
});

export const sessionExpiredLoginPath = "/login-local?reason=session_expired";

export async function localLoginPath(unauthorized: Response) {
  const body = (await unauthorized.json().catch(() => null)) as {
    code?: string;
  } | null;
  return body?.code === "session_expired" ? sessionExpiredLoginPath : "/login-local";
}
