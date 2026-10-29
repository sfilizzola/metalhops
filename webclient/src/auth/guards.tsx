import { Navigate, Outlet } from "react-router-dom";
import { MeProvider } from "../profile/MeProvider";
import { AppShell } from "../shell/AppShell";
import { useSession } from "./AuthProvider";

// Layout route for signed-in pages: the profile context and the shell wrap every one of them.
export function RequireAuth() {
  const session = useSession();
  if (!session) return <Navigate to="/login" replace />;
  return (
    <MeProvider userId={session.user.id}>
      <AppShell />
    </MeProvider>
  );
}

export function RedirectIfAuthed() {
  return useSession() ? <Navigate to="/" replace /> : <Outlet />;
}
