import { useState, type FormEvent } from 'react';
import { ShieldCheck, Sparkles, UserPlus, LogIn, Compass } from 'lucide-react';
import { AuthApiError, loginAccount, registerAccount } from '../../net/authApi';
import { wsClient } from '../../net/wsClient';
import { startOfflinePreview } from '../../net/gameSession';
import { useSessionStore } from '../../store/sessionStore';

export function LoginScreen() {
  const [mode, setMode] = useState<'login' | 'register'>('login');
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [email, setEmail] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [info, setInfo] = useState<string | null>(null);

  const setAccount = useSessionStore((s) => s.setAccount);
  const setConnected = useSessionStore((s) => s.setConnected);
  const setScreen = useSessionStore((s) => s.setScreen);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    setInfo(null);
    setLoading(true);

    try {
      if (mode === 'register') {
        await registerAccount(username, password, email);
        setInfo('Conta criada com Argon2id! Agora faça login.');
        setMode('login');
        setPassword('');
        return;
      }

      const auth = await loginAccount(username, password);
      await wsClient.connect(auth.ticket, 'tcn/line1');
      setAccount(auth.accountId, auth.username, false);
      setConnected(true);
      await wsClient.call('sendCharList', []);
      setScreen('charSelect');
    } catch (err) {
      if (err instanceof AuthApiError) {
        if (err.status === 429 && err.retryAfterSecs) {
          setError(`Muitas tentativas. Aguarde ${err.retryAfterSecs}s e tente novamente.`);
        } else {
          setError(err.message);
        }
      } else if (err instanceof Error) {
        setError(err.message);
      } else {
        setError('Erro inesperado ao conectar');
      }
    } finally {
      setLoading(false);
    }
  }

  return (
    <div
      className="relative flex h-full w-full items-center justify-center bg-slate-950 bg-cover bg-center p-4"
      style={{ backgroundImage: 'url(/assets/ui/starrysky.jpg)' }}
    >
      <div className="absolute inset-0 bg-slate-950/75 backdrop-blur-xs" />

      <div className="relative z-10 w-full max-w-md rounded-2xl border border-indigo-400/25 bg-slate-900/90 p-7 shadow-2xl shadow-indigo-950/60 backdrop-blur-md">
        <div className="mb-6 text-center">
          <div className="mx-auto mb-3 flex h-12 w-12 items-center justify-center rounded-xl bg-gradient-to-br from-indigo-500 to-cyan-400 text-slate-950 shadow-lg shadow-indigo-500/30">
            <Sparkles className="h-6 w-6" />
          </div>
          <h1 className="text-2xl font-bold tracking-tight text-white">MCGame Web</h1>
          <p className="mt-1 text-xs text-slate-400">
            Cliente HTML5 / PixiJS v8 • WebSocket + Argon2id
          </p>
        </div>

        <div className="mb-5 grid grid-cols-2 gap-1 rounded-lg bg-slate-950/80 p-1">
          <button
            type="button"
            onClick={() => {
              setMode('login');
              setError(null);
            }}
            className={`flex items-center justify-center gap-1.5 rounded-md py-2 text-sm font-medium transition ${
              mode === 'login'
                ? 'bg-indigo-600 text-white shadow'
                : 'text-slate-400 hover:text-slate-200'
            }`}
          >
            <LogIn className="h-4 w-4" /> Entrar
          </button>
          <button
            type="button"
            onClick={() => {
              setMode('register');
              setError(null);
            }}
            className={`flex items-center justify-center gap-1.5 rounded-md py-2 text-sm font-medium transition ${
              mode === 'register'
                ? 'bg-indigo-600 text-white shadow'
                : 'text-slate-400 hover:text-slate-200'
            }`}
          >
            <UserPlus className="h-4 w-4" /> Criar Conta
          </button>
        </div>

        {error && (
          <div className="mb-4 rounded-lg border border-rose-500/40 bg-rose-950/60 px-3 py-2 text-xs text-rose-200">
            {error}
          </div>
        )}
        {info && (
          <div className="mb-4 rounded-lg border border-emerald-500/40 bg-emerald-950/60 px-3 py-2 text-xs text-emerald-200">
            {info}
          </div>
        )}

        <form onSubmit={handleSubmit} className="space-y-4">
          <div>
            <label className="mb-1 block text-xs font-medium text-slate-300">Usuário</label>
            <input
              type="text"
              required
              minLength={3}
              maxLength={32}
              autoComplete="username"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              placeholder="ex: heroi_01"
              className="w-full rounded-lg border border-slate-700 bg-slate-950/90 px-3 py-2.5 text-sm text-white outline-none focus:border-indigo-400"
            />
          </div>

          {mode === 'register' && (
            <div>
              <label className="mb-1 block text-xs font-medium text-slate-300">
                E-mail (opcional)
              </label>
              <input
                type="email"
                autoComplete="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                placeholder="voce@exemplo.com"
                className="w-full rounded-lg border border-slate-700 bg-slate-950/90 px-3 py-2.5 text-sm text-white outline-none focus:border-indigo-400"
              />
            </div>
          )}

          <div>
            <label className="mb-1 block text-xs font-medium text-slate-300">Senha</label>
            <input
              type="password"
              required
              minLength={mode === 'register' ? 8 : 1}
              maxLength={128}
              autoComplete={mode === 'login' ? 'current-password' : 'new-password'}
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder={mode === 'register' ? 'Mínimo 8 caracteres' : '••••••••'}
              className="w-full rounded-lg border border-slate-700 bg-slate-950/90 px-3 py-2.5 text-sm text-white outline-none focus:border-indigo-400"
            />
          </div>

          <button
            type="submit"
            disabled={loading}
            className="w-full rounded-lg bg-gradient-to-r from-indigo-600 to-cyan-600 py-2.5 text-sm font-semibold text-white shadow-lg shadow-indigo-900/50 transition hover:from-indigo-500 hover:to-cyan-500 disabled:opacity-50"
          >
            {loading
              ? 'Conectando...'
              : mode === 'login'
                ? 'Entrar no Servidor'
                : 'Registrar Conta Segura'}
          </button>
        </form>

        <div className="mt-5 border-t border-slate-800 pt-4">
          <button
            type="button"
            onClick={() => startOfflinePreview(1)}
            className="flex w-full items-center justify-center gap-2 rounded-lg border border-slate-700 bg-slate-950/70 py-2 text-xs font-medium text-slate-300 transition hover:border-cyan-500/50 hover:text-cyan-200"
          >
            <Compass className="h-4 w-4" /> Explorar Mapas Extraídos (Preview Offline)
          </button>

          <div className="mt-3 flex items-center justify-center gap-1.5 text-[11px] text-slate-500">
            <ShieldCheck className="h-3.5 w-3.5 text-emerald-400" />
            <span>Sessão protegida por Ticket único (30s) & servidor autoritativo</span>
          </div>
        </div>
      </div>
    </div>
  );
}
