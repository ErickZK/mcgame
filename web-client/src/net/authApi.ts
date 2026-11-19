import { LoginResponseSchema, type LoginResponse } from './protocol';

export class AuthApiError extends Error {
  status: number;
  retryAfterSecs?: number;

  constructor(message: string, status: number, retryAfterSecs?: number) {
    super(message);
    this.status = status;
    this.retryAfterSecs = retryAfterSecs;
  }
}

export async function loginAccount(username: string, password: string): Promise<LoginResponse> {
  const res = await fetch('/api/auth/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username: username.trim(), password }),
  });

  const payload = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new AuthApiError(
      typeof payload.error === 'string' ? payload.error : 'Falha ao autenticar',
      res.status,
      typeof payload.retryAfterSecs === 'number' ? payload.retryAfterSecs : undefined,
    );
  }

  return LoginResponseSchema.parse(payload);
}

export async function registerAccount(
  username: string,
  password: string,
  email: string,
): Promise<void> {
  const res = await fetch('/api/auth/register', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      username: username.trim(),
      password,
      email: email.trim(),
    }),
  });

  const payload = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new AuthApiError(
      typeof payload.error === 'string' ? payload.error : 'Falha ao criar conta',
      res.status,
      typeof payload.retryAfterSecs === 'number' ? payload.retryAfterSecs : undefined,
    );
  }
}
