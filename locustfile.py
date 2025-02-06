from locust import HttpUser, task
from random import choice

class TestGetRoutes(HttpUser):
    project_set = None
    project_keys = None

    @task
    def test_project(self):
        response = self.client.get("/project/")
        if self.project_set is None:
            self.project_set = response.json()
            self.project_keys = self.project_set.keys()

    @task
    def test_solution(self):
        if self.project_set is not None:
            project = random.choice(self.project_keys)
            self.client.get(f"/project/{project}/models/")
