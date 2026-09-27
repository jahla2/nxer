export type ConsoleModule=
  |"Overview"
  |"Projects"
  |"API Keys"
  |"Models"
  |"Playground"
  |"Usage"
  |"Requests"
  |"Status"
  |"Docs"
  |"Settings"
  |"Admin";

export const MODULE_PATHS:Record<ConsoleModule,string>={
  "Overview":"/overview",
  "Projects":"/projects",
  "API Keys":"/api-keys",
  "Models":"/models",
  "Playground":"/playground",
  "Usage":"/usage",
  "Requests":"/requests",
  "Status":"/status",
  "Docs":"/docs",
  "Settings":"/settings",
  "Admin":"/admin",
};

const PATH_MODULES=new Map(
  Object.entries(MODULE_PATHS).map(([module,path])=>[path,module as ConsoleModule]),
);

export function pathForModule(module:ConsoleModule):string{
  return MODULE_PATHS[module];
}

export function moduleForPath(pathname:string):ConsoleModule{
  const normalized=pathname.length>1?pathname.replace(/\/+$/,""):pathname;
  return PATH_MODULES.get(normalized)??"Overview";
}

export type AuthRouteMode="login"|"register"|"forgot"|"reset"|"verify";

export const AUTH_PATHS:Record<AuthRouteMode,string>={
  login:"/login",
  register:"/register",
  forgot:"/forgot-password",
  reset:"/reset-password",
  verify:"/verify-email",
};
