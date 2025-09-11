package morphoroutes

import (
	"log"
	"path"
	"time"
)

func (s *Service) BackgroundJob(job func(ctx map[string]any, service Service) error, period time.Duration, ctx map[string]any) {
	for {
		err := job(ctx, *s)
		if err != nil {
			log.Println(err)
		}
		time.Sleep(period)
	}
}

func RepopulateCSV(ctx map[string]any, service Service) error {
	log.Println("starting csv repopulation job...")
	db, err := service.GetDB()
	if err != nil {
		return NewServerError(err)
	}

	projects, err := GetAllProjects(db)
	if err != nil {
		return NewServerError(err)
	}

	for _, project := range projects {
		if CheckAssetExistenceS3(service, path.Join("assets", project.ProjectName, "data.csv")) || CheckAssetExistenceS3(service, path.Join("assets", project.ProjectName, "data_api.csv")) {
			continue
		}

		log.Printf("writing csv data for %s...\n", project.ProjectName)
		UploadCsv(service, project.ProjectName)
	}

	return nil
}

func RepopulateArchiveZip(ctx map[string]any, service Service) error {
	log.Println("starting archival job...")
	db, err := service.GetDB()
	if err != nil {
		return NewServerError(err)
	}

	projects, err := GetAllProjects(db)
	if err != nil {
		return NewServerError(err)
	}

	for _, project := range projects {
		if CheckAssetExistenceS3(service, path.Join("assets", project.ProjectName, "archive.zip")) {
			continue
		}

		log.Printf("started writing archive for %s...\n", project.ProjectName)
		err = UploadArchive(service, project.ProjectName)
		if err != nil {
			log.Println(err)
		}
		log.Printf("wrote archive for %s.\n", project.ProjectName)
	}

	return nil
}
