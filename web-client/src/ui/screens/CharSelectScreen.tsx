import { useState, type FormEvent } from 'react';
import { LogOut, Plus, Play, Shield } from 'lucide-react';
import { GAME_CLASSES, getClassInfo, type CharacterSummary } from '../../net/protocol';
import { wsClient } from '../../net/wsClient';
import { enterWorldWithCharacter } from '../../net/gameSession';
import { useSessionStore } from '../../store/sessionStore';
import { useWorldStore } from '../../store/worldStore';

export function CharSelectScreen() {
  const username = useSessionStore((s) => s.username);
  const characters = useSessionStore((s) => s.characters);
  const resetSession = useSessionStore((s) => s.resetSession);
  const resetWorld = useWorldStore((s) => s.resetWorld);

  const [selectedId, setSelectedId] = useState<number | null>(characters[0]?.id ?? null);
  const [creating, setCreating] = useState(false);
  const [newName, setNewName] = useState('');
  const [newClassId, setNewClassId] = useState<number>(1);
  const [newGender, setNewGender] = useState<number>(0);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const activeChar: CharacterSummary | undefined =
    characters.find((c) => c.id === selectedId) ?? characters[0];

  async function handleEnter() {
    if (!activeChar) return;
    setBusy(true);
    setError(null);
    try {
      await enterWorldWithCharacter(activeChar);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Erro ao entrar no mundo');
    } finally {
      setBusy(false);
    }
  }

  async function handleCreate(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await wsClient.call('newChar', [
        {
          name: newName.trim(),
          gender: newGender,
          classData: { classId: newClassId },
        },
      ]);
      await wsClient.call('sendCharList', []);
      setNewName('');
      setCreating(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Não foi possível criar o personagem');
    } finally {
      setBusy(false);
    }
  }

  function handleLogout() {
    wsClient.disconnect();
    resetWorld();
    resetSession();
  }

  return (
    <div
      className="relative flex h-full w-full items-center justify-center bg-slate-950 bg-cover bg-center p-4"
      style={{ backgroundImage: 'url(/assets/ui/soulbg.jpg)' }}
    >
      <div className="absolute inset-0 bg-slate-950/80 backdrop-blur-xs" />

      <div className="relative z-10 w-full max-w-3xl rounded-2xl border border-indigo-400/25 bg-slate-900/90 p-6 shadow-2xl backdrop-blur-md">
        <div className="mb-6 flex items-center justify-between border-b border-slate-800 pb-4">
          <div>
            <h2 className="text-xl font-bold text-white">Seleção de Personagem</h2>
            <p className="text-xs text-slate-400">Conta conectada: {username}</p>
          </div>
          <button
            type="button"
            onClick={handleLogout}
            className="flex items-center gap-1.5 rounded-lg border border-slate-700 px-3 py-1.5 text-xs text-slate-300 hover:border-rose-500/50 hover:text-rose-200"
          >
            <LogOut className="h-3.5 w-3.5" /> Sair
          </button>
        </div>

        {error && (
          <div className="mb-4 rounded-lg border border-rose-500/40 bg-rose-950/60 px-3 py-2 text-xs text-rose-200">
            {error}
          </div>
        )}

        {!creating ? (
          <>
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 md:grid-cols-3">
              {characters.map((ch) => {
                const cls = getClassInfo(ch.classId);
                const isSelected = activeChar?.id === ch.id;
                return (
                  <button
                    key={ch.id}
                    type="button"
                    onClick={() => setSelectedId(ch.id)}
                    className={`rounded-xl border p-4 text-left transition ${
                      isSelected
                        ? 'border-cyan-400 bg-indigo-950/70 shadow-lg shadow-cyan-950/40'
                        : 'border-slate-800 bg-slate-950/60 hover:border-slate-600'
                    }`}
                  >
                    <div className="flex items-center justify-between">
                      <span className="font-semibold text-white">{ch.name}</span>
                      <span className="rounded bg-slate-800 px-2 py-0.5 text-xs text-amber-300">
                        Nv. {ch.level}
                      </span>
                    </div>
                    <p className="mt-1 text-xs" style={{ color: cls.color }}>
                      {cls.name}
                    </p>
                    <p className="mt-2 text-[11px] text-slate-500">
                      {ch.gender === 1 ? 'Feminino' : 'Masculino'}
                    </p>
                  </button>
                );
              })}

              <button
                type="button"
                onClick={() => setCreating(true)}
                className="flex min-h-[104px] flex-col items-center justify-center gap-2 rounded-xl border border-dashed border-slate-700 bg-slate-950/40 text-xs font-medium text-slate-400 transition hover:border-indigo-400 hover:text-indigo-200"
              >
                <Plus className="h-5 w-5" /> Criar Novo Personagem
              </button>
            </div>

            <div className="mt-6 flex justify-end">
              <button
                type="button"
                disabled={!activeChar || busy}
                onClick={handleEnter}
                className="flex items-center gap-2 rounded-lg bg-gradient-to-r from-indigo-600 to-cyan-600 px-6 py-2.5 text-sm font-semibold text-white shadow-lg transition hover:from-indigo-500 hover:to-cyan-500 disabled:opacity-40"
              >
                <Play className="h-4 w-4" />
                {busy ? 'Carregando mapa...' : 'Entrar no Mundo'}
              </button>
            </div>
          </>
        ) : (
          <form onSubmit={handleCreate} className="space-y-4">
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
              <div>
                <label className="mb-1 block text-xs font-medium text-slate-300">
                  Nome do Personagem
                </label>
                <input
                  type="text"
                  required
                  minLength={2}
                  maxLength={20}
                  value={newName}
                  onChange={(e) => setNewName(e.target.value)}
                  placeholder="Ex: ErickMago"
                  className="w-full rounded-lg border border-slate-700 bg-slate-950 px-3 py-2 text-sm text-white outline-none focus:border-cyan-400"
                />
              </div>

              <div>
                <label className="mb-1 block text-xs font-medium text-slate-300">Gênero</label>
                <div className="grid grid-cols-2 gap-2">
                  {[
                    { val: 0, label: 'Masculino' },
                    { val: 1, label: 'Feminino' },
                  ].map((g) => (
                    <button
                      key={g.val}
                      type="button"
                      onClick={() => setNewGender(g.val)}
                      className={`rounded-lg border py-2 text-xs font-medium ${
                        newGender === g.val
                          ? 'border-cyan-400 bg-cyan-950/60 text-cyan-200'
                          : 'border-slate-700 bg-slate-950 text-slate-400'
                      }`}
                    >
                      {g.label}
                    </button>
                  ))}
                </div>
              </div>
            </div>

            <div>
              <label className="mb-2 block text-xs font-medium text-slate-300">Classe</label>
              <div className="grid grid-cols-2 gap-2 sm:grid-cols-3">
                {GAME_CLASSES.map((cls) => (
                  <button
                    key={cls.id}
                    type="button"
                    onClick={() => setNewClassId(cls.id)}
                    className={`rounded-lg border p-2.5 text-left transition ${
                      newClassId === cls.id
                        ? 'border-cyan-400 bg-indigo-950/70'
                        : 'border-slate-800 bg-slate-950/70 hover:border-slate-600'
                    }`}
                  >
                    <div className="flex items-center gap-1.5 text-xs font-semibold" style={{ color: cls.color }}>
                      <Shield className="h-3.5 w-3.5" /> {cls.name}
                    </div>
                    <p className="mt-1 text-[11px] text-slate-400">{cls.role}</p>
                  </button>
                ))}
              </div>
            </div>

            <div className="flex justify-end gap-2 pt-2">
              <button
                type="button"
                onClick={() => setCreating(false)}
                className="rounded-lg border border-slate-700 px-4 py-2 text-xs text-slate-300 hover:bg-slate-800"
              >
                Cancelar
              </button>
              <button
                type="submit"
                disabled={busy}
                className="rounded-lg bg-indigo-600 px-5 py-2 text-xs font-semibold text-white hover:bg-indigo-500 disabled:opacity-50"
              >
                {busy ? 'Criando...' : 'Criar Personagem'}
              </button>
            </div>
          </form>
        )}
      </div>
    </div>
  );
}
