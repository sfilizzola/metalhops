import { Navigate, Route, Routes } from "react-router-dom";
import { AuthPage } from "./auth/AuthPage";
import { AuthProvider, useSession } from "./auth/AuthProvider";
import { RedirectIfAuthed, RequireAuth } from "./auth/guards";
import { CheckIns } from "./pages/CheckIns";
import { Discover } from "./pages/Discover";
import { Home } from "./pages/Home";
import { MyVenues } from "./pages/MyVenues";
import { ProfileEditPage } from "./profile/ProfileEditPage";
import { ProfilePage } from "./profile/ProfilePage";
import { apiBaseUrl, supabase } from "./supabase";
import { Alert } from "./ui/Alert";
import { Spinner } from "./ui/Spinner";

function ProfileRoute() {
  return <ProfilePage email={useSession()?.user.email ?? ""} />;
}

function ProfileEditRoute() {
  return <ProfileEditPage email={useSession()?.user.email ?? ""} />;
}

function AppRoutes() {
  const session = useSession();
  if (session === undefined) {
    return <main className="auth"><p role="status"><Spinner /> Loading…</p></main>;
  }
  return (
    <Routes>
      <Route element={<RedirectIfAuthed />}>
        <Route path="/login" element={<AuthPage mode="in" />} />
        <Route path="/signup" element={<AuthPage mode="up" />} />
      </Route>
      <Route element={<RequireAuth />}>
        <Route path="/" element={<Home />} />
        <Route path="/discover" element={<Discover />} />
        <Route path="/check-ins" element={<CheckIns />} />
        <Route path="/my-venues" element={<MyVenues />} />
        <Route path="/profile" element={<ProfileRoute />} />
        <Route path="/profile/edit" element={<ProfileEditRoute />} />
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}

export default function App() {
  // The token goes to apiBaseUrl, so outside dev it must be https.
  const apiOk = !!apiBaseUrl && (import.meta.env.DEV || apiBaseUrl.startsWith("https://"));
  if (!supabase || !apiOk) {
    return (
      <main className="auth">
        <Alert kind="error">Missing configuration: set VITE_SUPABASE_URL, VITE_SUPABASE_PUBLISHABLE_KEY and VITE_API_BASE_URL (https outside development).</Alert>
      </main>
    );
  }
  return (
    <AuthProvider>
      <AppRoutes />
    </AuthProvider>
  );
}
