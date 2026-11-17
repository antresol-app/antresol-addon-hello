// The app provides react-router to a bundle at run time (the SDK's vite
// plugin keeps it external); only what this addon uses is typed here.
declare module 'react-router' {
  export function useNavigate(): (to: string) => void
}
