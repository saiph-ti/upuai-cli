package detect

type Framework struct {
	Name      string
	Files     []string
	BuildCmd  string
	StartCmd  string
	OutputDir string
}

var Frameworks = []Framework{
	{
		Name:      "Next.js",
		Files:     []string{"next.config.js", "next.config.mjs", "next.config.ts"},
		BuildCmd:  "npm run build",
		StartCmd:  "npm start",
		OutputDir: ".next",
	},
	{
		Name:      "Vite",
		Files:     []string{"vite.config.ts", "vite.config.js"},
		BuildCmd:  "npm run build",
		StartCmd:  "npm run preview",
		OutputDir: "dist",
	},
	{
		Name:      "React (CRA)",
		Files:     []string{"public/index.html"},
		BuildCmd:  "npm run build",
		StartCmd:  "npx serve -s build",
		OutputDir: "build",
	},
	{
		Name:     "Node.js (Express)",
		Files:    []string{"package.json"},
		BuildCmd: "npm install",
		StartCmd: "npm start",
	},
	{
		Name:     "Go",
		Files:    []string{"go.mod"},
		BuildCmd: "go build -o app .",
		StartCmd: "./app",
	},
	{
		Name:     "Python (Django)",
		Files:    []string{"manage.py"},
		BuildCmd: "pip install -r requirements.txt",
		StartCmd: "python manage.py runserver",
	},
	{
		Name:     "Python (Flask)",
		Files:    []string{"app.py", "wsgi.py"},
		BuildCmd: "pip install -r requirements.txt",
		StartCmd: "flask run",
	},
	{
		Name:     "Python",
		Files:    []string{"requirements.txt", "pyproject.toml"},
		BuildCmd: "pip install -r requirements.txt",
		StartCmd: "python main.py",
	},
	{
		Name:     "Ruby on Rails",
		Files:    []string{"Gemfile", "config/routes.rb"},
		BuildCmd: "bundle install",
		StartCmd: "rails server",
	},
	{
		Name:     "Docker",
		Files:    []string{"Dockerfile"},
		BuildCmd: "docker build -t app .",
		StartCmd: "docker run app",
	},
	{
		Name:     "Static",
		Files:    []string{"index.html"},
		BuildCmd: "",
		StartCmd: "npx serve .",
	},
}
